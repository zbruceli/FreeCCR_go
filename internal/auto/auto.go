// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Package auto ports FreeCCR's assisted tools: the WB eyedropper
// (compute_neutral_temp_tint), auto-exposure (compute_auto_exposure_gain), and
// the classical auto-white-balance estimators (core/awb.py). Auto tools are
// estimates, so exact numeric parity with numpy is not required.
package auto

import (
	"math"
	"sort"

	"github.com/zhengli/freeccr-go/internal/image"
)

const (
	wsB        = 512.0
	wsInvWidth = 1.0 / 1024.0
	minContent = 0.005
	wbTemp     = 0.40 // _WB_TEMP_STRENGTH
	wbTint     = 0.26 // _WB_TINT_STRENGTH
)

// NeutralTempTint returns the (temperature, tint) slider values that make an
// RGB sample neutral after adjust_image's WB stage. Mirrors
// compute_neutral_temp_tint (ccr_processor.py:2275). Only channel ratios matter.
func NeutralTempTint(r, g, b, balance float64) (int, int) {
	const eps = 1e-6
	r, g, b = math.Max(r, eps), math.Max(g, eps), math.Max(b, eps)
	s := (b - r) / (b + r)
	tempF := clamp(s*100.0/wbTemp, -100, 100)
	sEff := tempF / 100.0 * wbTemp
	m := (r*(1+sEff) + b*(1-sEff)) / 2.0
	t := (g - m) / (g + 0.3*m)
	denom := wbTint * balance
	x := clamp(t/math.Max(denom, eps), -0.999, 0.999)
	tintF := clamp(math.Atanh(x)/0.02, -100, 100)
	return int(math.Round(tempF)), int(math.Round(tintF))
}

// Auto-exposure constants (ccr_processor.py:1124-1130).
const (
	aePercentile   = 98.0
	aeTarget       = 0.98
	aeWhiteExclude = 0.99
)

// AutoExposureGain returns the exposure-base slider value that places the P98
// (holder-excluded) luminance at 98% of full scale. Mirrors
// compute_auto_exposure_gain (ccr_processor.py:1133). im is the converted base.
func AutoExposureGain(im *image.Image, wsWindowed bool) float64 {
	n := im.W * im.H
	lums := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		r, g, b := display(im.Pix, i*3, wsWindowed)
		lum := 0.299*r + 0.587*g + 0.114*b
		if lum < aeWhiteExclude*65535.0 {
			lums = append(lums, lum)
		}
	}
	if len(lums) < int(minContent*float64(n)) {
		return 0
	}
	v98 := percentile(lums, aePercentile)
	if v98 <= 1.0 {
		return 0
	}
	gain := aeTarget * 65535.0 / v98
	base := 50.0 * math.Log2(gain)
	return clamp(base, -100, 100)
}

// AWB estimators (core/awb.py).
const (
	awbLo   = 0.02
	awbHi   = 0.98
	awbEps  = 1e-6
	wpPct   = 99.0
	minkP   = 6.0
	geSigma = 1.0
)

// AWBAlgorithms are the estimator ids in UI order.
var AWBAlgorithms = []string{"gray_world", "white_patch", "shades_of_gray", "gray_edge"}

// AWBTempTint estimates the cast of a converted base and returns the temp/tint
// sliders that neutralize it. ok=false when there isn't enough usable content.
func AWBTempTint(im *image.Image, wsWindowed bool, algo string, balance float64) (int, int, bool) {
	r, g, b, ok := EstimateNeutralRGB(im, wsWindowed, algo)
	if !ok {
		return 0, 0, false
	}
	t, ti := NeutralTempTint(r, g, b, balance)
	return t, ti, true
}

// EstimateNeutralRGB returns the RGB triple that should be neutral, per the
// chosen algorithm. Mirrors estimate_neutral_rgb (awb.py). Scale is arbitrary.
func EstimateNeutralRGB(im *image.Image, wsWindowed bool, algo string) (float64, float64, float64, bool) {
	n := im.W * im.H
	// Normalized image + valid mask.
	norm := make([]float64, n*3)
	valid := make([]bool, n)
	nValid := 0
	for i := 0; i < n; i++ {
		o := i * 3
		v0, v1, v2 := norm01(im.Pix, o, wsWindowed)
		norm[o], norm[o+1], norm[o+2] = v0, v1, v2
		if v0 >= awbLo && v0 <= awbHi && v1 >= awbLo && v1 <= awbHi && v2 >= awbLo && v2 <= awbHi {
			valid[i] = true
			nValid++
		}
	}
	if nValid < int(minContent*float64(n)) {
		return 0, 0, 0, false
	}

	var est [3]float64
	switch algo {
	case "white_patch":
		for c := 0; c < 3; c++ {
			vals := collect(norm, valid, n, c)
			est[c] = percentile(vals, wpPct)
		}
	case "shades_of_gray":
		est = minkowski(norm, valid, n)
	case "gray_edge":
		g, ok := grayEdge(im, norm, valid, wsWindowed)
		if !ok {
			return 0, 0, 0, false
		}
		est = g
	default: // gray_world
		var sum [3]float64
		for i := 0; i < n; i++ {
			if valid[i] {
				o := i * 3
				sum[0] += norm[o]
				sum[1] += norm[o+1]
				sum[2] += norm[o+2]
			}
		}
		for c := 0; c < 3; c++ {
			est[c] = sum[c] / float64(nValid)
		}
	}
	for c := 0; c < 3; c++ {
		if math.IsNaN(est[c]) || math.IsInf(est[c], 0) || est[c] <= awbEps {
			return 0, 0, 0, false
		}
	}
	return est[0], est[1], est[2], true
}

// minkowski computes the per-channel Minkowski p-mean over valid pixels.
func minkowski(norm []float64, valid []bool, n int) [3]float64 {
	var sum [3]float64
	cnt := 0
	for i := 0; i < n; i++ {
		if valid[i] {
			o := i * 3
			sum[0] += math.Pow(norm[o], minkP)
			sum[1] += math.Pow(norm[o+1], minkP)
			sum[2] += math.Pow(norm[o+2], minkP)
			cnt++
		}
	}
	var est [3]float64
	for c := 0; c < 3; c++ {
		est[c] = math.Pow(sum[c]/float64(cnt), 1.0/minkP)
	}
	return est
}

// grayEdge: Minkowski p-mean of the smoothed per-channel gradient magnitude over
// valid pixels (van de Weijer). Approximates awb._gray_edge_estimate.
func grayEdge(im *image.Image, norm []float64, valid []bool, wsWindowed bool) ([3]float64, bool) {
	w, h := im.W, im.H
	sm := gaussian3(norm, w, h, geSigma)
	var sum [3]float64
	cnt := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			if !valid[i] {
				continue
			}
			for c := 0; c < 3; c++ {
				gx := grad(sm, w, h, x, y, c, 1, 0)
				gy := grad(sm, w, h, x, y, c, 0, 1)
				mag := math.Sqrt(gx*gx + gy*gy)
				sum[c] += math.Pow(mag, minkP)
			}
			cnt++
		}
	}
	if cnt == 0 {
		return [3]float64{}, false
	}
	var est [3]float64
	for c := 0; c < 3; c++ {
		est[c] = math.Pow(sum[c]/float64(cnt), 1.0/minkP)
	}
	return est, true
}

// grad central difference along (dx,dy) for channel c (edge-clamped).
func grad(a []float64, w, h, x, y, c, dx, dy int) float64 {
	x0, x1 := clampi(x-dx, 0, w-1), clampi(x+dx, 0, w-1)
	y0, y1 := clampi(y-dy, 0, h-1), clampi(y+dy, 0, h-1)
	span := 2.0
	if x0 == x1 && y0 == y1 {
		return 0
	}
	if (dx != 0 && (x-dx < 0 || x+dx > w-1)) || (dy != 0 && (y-dy < 0 || y+dy > h-1)) {
		span = 1.0
	}
	return (a[(y1*w+x1)*3+c] - a[(y0*w+x0)*3+c]) / span
}

// gaussian3 blurs a 3-channel interleaved float image with a small separable
// Gaussian (edge-clamped). Coarse — adequate for the gray-edge estimate.
func gaussian3(a []float64, w, h int, sigma float64) []float64 {
	r := int(math.Ceil(sigma * 3))
	if r < 1 {
		r = 1
	}
	k := make([]float64, 2*r+1)
	var ksum float64
	for i := -r; i <= r; i++ {
		k[i+r] = math.Exp(-float64(i*i) / (2 * sigma * sigma))
		ksum += k[i+r]
	}
	for i := range k {
		k[i] /= ksum
	}
	tmp := make([]float64, len(a))
	out := make([]float64, len(a))
	for y := 0; y < h; y++ { // horizontal
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				var s float64
				for t := -r; t <= r; t++ {
					s += k[t+r] * a[(y*w+clampi(x+t, 0, w-1))*3+c]
				}
				tmp[(y*w+x)*3+c] = s
			}
		}
	}
	for y := 0; y < h; y++ { // vertical
		for x := 0; x < w; x++ {
			for c := 0; c < 3; c++ {
				var s float64
				for t := -r; t <= r; t++ {
					s += k[t+r] * tmp[(clampi(y+t, 0, h-1)*w+x)*3+c]
				}
				out[(y*w+x)*3+c] = s
			}
		}
	}
	return out
}

// --- helpers ---

// norm01 returns a pixel's channels in [0,1] (de-windowed if working-space).
func norm01(pix []float32, o int, ws bool) (float64, float64, float64) {
	if ws {
		return (float64(pix[o]) - wsB) * wsInvWidth,
			(float64(pix[o+1]) - wsB) * wsInvWidth,
			(float64(pix[o+2]) - wsB) * wsInvWidth
	}
	return float64(pix[o]) / 65535.0, float64(pix[o+1]) / 65535.0, float64(pix[o+2]) / 65535.0
}

// display returns a pixel's channels on the [0,65535] display scale (windowed
// bases are de-windowed and clamped to the window).
func display(pix []float32, o int, ws bool) (float64, float64, float64) {
	if ws {
		return clamp01v((float64(pix[o])-wsB)*wsInvWidth) * 65535,
			clamp01v((float64(pix[o+1])-wsB)*wsInvWidth) * 65535,
			clamp01v((float64(pix[o+2])-wsB)*wsInvWidth) * 65535
	}
	return float64(pix[o]), float64(pix[o+1]), float64(pix[o+2])
}

func collect(norm []float64, valid []bool, n, c int) []float64 {
	out := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		if valid[i] {
			out = append(out, norm[i*3+c])
		}
	}
	return out
}

func percentile(data []float64, q float64) float64 {
	if len(data) == 0 {
		return 0
	}
	s := make([]float64, len(data))
	copy(s, data)
	sort.Float64s(s)
	if len(s) == 1 {
		return s[0]
	}
	rank := q / 100.0 * float64(len(s)-1)
	lo := int(math.Floor(rank))
	if lo >= len(s)-1 {
		return s[len(s)-1]
	}
	return s[lo] + (rank-float64(lo))*(s[lo+1]-s[lo])
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func clamp01v(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
