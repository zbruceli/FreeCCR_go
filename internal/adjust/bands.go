// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package adjust

import (
	"math"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// Per-color-band adjustments (ccr_processor.py:2540-2762): 7 hue-sector bands,
// each with subsat/sat/bright/hue in [-100,100]. Selection is on the ORIGINAL
// hue; sat/bright/hue act in HSV, subsat is the subtractive-saturation model.
// The HSV round-trip replicates OpenCV's float RGB2HSV/HSV2RGB exactly
// (including FLT_EPSILON and the sector table) so output tracks the cv2 pipeline.

// Band centers on the HSV wheel, in wheel order matching COLOR_BANDS
// (red, skin, yellow, green, cyan, blue, purple).
var bandCenters = [7]float64{0, 28, 58, 120, 180, 240, 300}

const (
	bandParamSubsat = 0
	bandParamSat    = 1
	bandParamBright = 2
	bandParamHue    = 3

	bandSatGateLo = 0.06
	bandSatGateHi = 0.20
	bandHueFull   = 30.0
	bandLUTBins   = 720
	fltEpsilon    = 1.1920929e-07 // FLT_EPSILON, as OpenCV uses
)

// BandSettings holds the 7×4 band parameter grid (param order:
// subsat, sat, bright, hue) plus the spatial feather amount.
type BandSettings struct {
	Bands   [7][4]float64
	Feather float64
}

// IsZero reports whether every band slider is 0 (a no-op).
func (b *BandSettings) IsZero() bool {
	if b == nil {
		return true
	}
	for i := 0; i < 7; i++ {
		for p := 0; p < 4; p++ {
			if b.Bands[i][p] != 0 {
				return false
			}
		}
	}
	return true
}

// bandBinLUT is the (720×4) table of blended per-param deltas by hue bin, with a
// per-param "active" flag. Mirrors _band_bin_lut + _band_param_deltas.
type bandBinLUT struct {
	delta  [bandLUTBins][4]float64
	active [4]bool
}

func buildBandBinLUT(bands [7][4]float64) *bandBinLUT {
	out := &bandBinLUT{}
	for p := 0; p < 4; p++ {
		for i := 0; i < 7; i++ {
			if bands[i][p] != 0 {
				out.active[p] = true
				break
			}
		}
	}
	// Blend per-band values over the hue wheel with a smoothstep ramp between
	// adjacent band centers (purple→red wraps through 360).
	for bin := 0; bin < bandLUTBins; bin++ {
		hue := float64(bin) * (360.0 / bandLUTBins)
		idx := searchsortedRight(bandCenters[:], hue) - 1
		if idx < 0 {
			idx = 0
		}
		last := 6
		nxt := idx + 1
		c1 := bandCenters[nxt%7]
		if idx == last {
			nxt = 0
			c1 = 360.0
		}
		c0 := bandCenters[idx]
		t := (hue - c0) / (c1 - c0)
		if t < 0 {
			t = 0
		} else if t > 1 {
			t = 1
		}
		ramp := t * t * (3.0 - 2.0*t)
		for p := 0; p < 4; p++ {
			out.delta[bin][p] = bands[idx][p]*(1.0-ramp) + bands[nxt][p]*ramp
		}
	}
	return out
}

// Spatial feather: at feather=100 the correction is low-passed with a Gaussian
// of this fraction of the long edge (ccr_processor.py:2641).
const bandFeatherMaxFrac = 0.012

// ApplyColorBands applies the per-band adjustments to a 16-bit image. Returns a
// clone unchanged when all sliders are 0. Mirrors apply_color_band_adjustments /
// _apply_color_bands_float. When feather>0 the band correction is spatially
// low-passed (Gaussian on the delta) before being re-added. Quantizes by
// truncation, matching the in-pipeline path.
func ApplyColorBands(im *image.Image, bs *BandSettings) *image.Image {
	if bs.IsZero() {
		return im.Clone()
	}
	lut := buildBandBinLUT(bs.Bands)

	long := im.W
	if im.H > long {
		long = im.H
	}
	feather := bs.Feather
	if feather > 100 {
		feather = 100
	}
	sigma := (feather / 100.0) * bandFeatherMaxFrac * float64(long)

	out := newLikeA(im)
	src, dst := im.Pix, out.Pix

	if feather <= 0 || sigma < 0.5 {
		// Fast path: per-pixel, no feather (matches Python's early return).
		par.Rows(im.H, func(lo, hi int) {
			for i := lo * im.W * 3; i < hi*im.W*3; i += 3 {
				r, g, b := bandPixel(src[i], src[i+1], src[i+2], lut)
				dst[i] = quantU16(r * 65535)
				dst[i+1] = quantU16(g * 65535)
				dst[i+2] = quantU16(b * 65535)
			}
		})
		return out
	}

	// Feather path: compute the correction delta (adjusted − original) in float,
	// low-pass it, re-add to the original, then quantize once.
	const inv = float32(1.0 / 65535.0)
	delta := image.GetBuf(len(src))
	par.Rows(im.H, func(lo, hi int) {
		for i := lo * im.W * 3; i < hi*im.W*3; i += 3 {
			or := clamp01f(src[i] * inv)
			og := clamp01f(src[i+1] * inv)
			ob := clamp01f(src[i+2] * inv)
			r, g, b := bandPixel(src[i], src[i+1], src[i+2], lut)
			delta[i] = r - or
			delta[i+1] = g - og
			delta[i+2] = b - ob
		}
	})
	gaussianBlur3(delta, im.W, im.H, sigma)
	par.Rows(im.H, func(lo, hi int) {
		for i := lo * im.W * 3; i < hi*im.W*3; i += 3 {
			dst[i] = quantU16(clamp01f(src[i]*inv+delta[i]) * 65535)
			dst[i+1] = quantU16(clamp01f(src[i+1]*inv+delta[i+1]) * 65535)
			dst[i+2] = quantU16(clamp01f(src[i+2]*inv+delta[i+2]) * 65535)
		}
	})
	image.PutBuf(delta)
	return out
}

// gaussianBlur3 low-passes a 3-channel interleaved float image in place with a
// separable Gaussian (cv2-style kernel, BORDER_REFLECT_101). Approximates
// cv2.GaussianBlur(ksize=0, sigma); the band feather is a subtle edge-softening
// so exact cv2 parity is not required (the feather=0 core is golden-verified).
func gaussianBlur3(buf []float32, w, h int, sigma float64) {
	k := gaussianKernel(sigma)
	r := len(k) / 2
	tmp := image.GetBuf(len(buf))
	defer image.PutBuf(tmp)

	// Horizontal pass: buf → tmp.
	par.Rows(h, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			row := y * w * 3
			for x := 0; x < w; x++ {
				var s0, s1, s2 float64
				for t := -r; t <= r; t++ {
					xi := reflect101(x+t, w)
					o := row + xi*3
					wt := k[t+r]
					s0 += wt * float64(buf[o])
					s1 += wt * float64(buf[o+1])
					s2 += wt * float64(buf[o+2])
				}
				o := row + x*3
				tmp[o] = float32(s0)
				tmp[o+1] = float32(s1)
				tmp[o+2] = float32(s2)
			}
		}
	})
	// Vertical pass: tmp → buf.
	par.Rows(h, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			for x := 0; x < w; x++ {
				var s0, s1, s2 float64
				for t := -r; t <= r; t++ {
					yi := reflect101(y+t, h)
					o := (yi*w + x) * 3
					wt := k[t+r]
					s0 += wt * float64(tmp[o])
					s1 += wt * float64(tmp[o+1])
					s2 += wt * float64(tmp[o+2])
				}
				o := (y*w + x) * 3
				buf[o] = float32(s0)
				buf[o+1] = float32(s1)
				buf[o+2] = float32(s2)
			}
		}
	})
}

// gaussianKernel builds a normalized 1-D Gaussian, with cv2's auto ksize rule
// for float images (ksize = round(8σ+1) | 1).
func gaussianKernel(sigma float64) []float64 {
	ksize := int(math.Round(sigma*8+1)) | 1
	if ksize < 3 {
		ksize = 3
	}
	c := (ksize - 1) / 2
	k := make([]float64, ksize)
	scale := -0.5 / (sigma * sigma)
	var sum float64
	for i := 0; i < ksize; i++ {
		x := float64(i - c)
		k[i] = math.Exp(scale * x * x)
		sum += k[i]
	}
	for i := range k {
		k[i] /= sum
	}
	return k
}

// reflect101 maps an out-of-range index using OpenCV's BORDER_REFLECT_101.
func reflect101(i, n int) int {
	if n == 1 {
		return 0
	}
	for i < 0 || i >= n {
		if i < 0 {
			i = -i
		}
		if i >= n {
			i = 2*(n-1) - i
		}
	}
	return i
}

// bandPixel runs the band chain on one pixel (16-bit input) and returns the
// adjusted RGB in [0,1].
func bandPixel(r16, g16, b16 float32, lut *bandBinLUT) (float32, float32, float32) {
	const inv = float32(1.0 / 65535.0)
	r := clamp01f(r16 * inv)
	g := clamp01f(g16 * inv)
	b := clamp01f(b16 * inv)

	h, s, v := rgb2hsvF(r, g, b)

	// Hue-bin lookup with linear interpolation.
	pos := h * float32(bandLUTBins) / 360.0
	bin0 := int(pos)
	if bin0 > bandLUTBins-1 {
		bin0 = bandLUTBins - 1
	}
	frac := pos - float32(bin0)
	bin1 := bin0 + 1
	if bin0 == bandLUTBins-1 {
		bin1 = 0
	}
	lookup := func(p int) float32 {
		return float32(lut.delta[bin0][p])*(1-frac) + float32(lut.delta[bin1][p])*frac
	}

	// Saturation gate (smoothstep).
	gg := (s - bandSatGateLo) / (bandSatGateHi - bandSatGateLo)
	gg = clamp01f(gg)
	gate := gg * gg * (3 - 2*gg)

	if lut.active[bandParamHue] {
		hd := lookup(bandParamHue)
		h = fmod360(h + hd*(bandHueFull/100.0)*gate)
	}
	if lut.active[bandParamSat] {
		sd := lookup(bandParamSat)
		f := 1 + sd/100.0*gate
		if f < 0 {
			f = 0
		}
		s = clamp01f(s * f)
	}
	if lut.active[bandParamBright] {
		bd := lookup(bandParamBright)
		v = clamp01f(v * float32(math.Exp2(float64(bd/100.0*gate))))
	}

	r, g, b = hsv2rgbF(h, s, v)

	if lut.active[bandParamSubsat] {
		strength := lookup(bandParamSubsat) * gate
		if strength != 0 {
			gamma := float32(math.Exp2(float64(strength / 100.0)))
			mx := r
			if g > mx {
				mx = g
			}
			if b > mx {
				mx = b
			}
			if mx > 1e-6 {
				r = subsatChan(r, mx, gamma)
				g = subsatChan(g, mx, gamma)
				b = subsatChan(b, mx, gamma)
			}
		}
	}
	return r, g, b
}

// subsatChan reproduces mx * exp(log(clipped_ratio) * gamma) with the ratio
// clamp/snap the Python uses (HSV round-trip noise floor at 1e-20).
func subsatChan(ch, mx, gamma float32) float32 {
	ratio := ch / mx
	if ratio < 1e-20 {
		ratio = 1e-20
	} else if ratio > 1 {
		ratio = 1
	}
	if ratio < 1e-6 {
		ratio = 1e-20
	}
	return mx * float32(math.Exp(math.Log(float64(ratio))*float64(gamma)))
}

// rgb2hsvF replicates OpenCV's RGB2HSV_f: H in [0,360), S,V in [0,1].
func rgb2hsvF(r, g, b float32) (h, s, v float32) {
	v = r
	if g > v {
		v = g
	}
	if b > v {
		v = b
	}
	vmin := r
	if g < vmin {
		vmin = g
	}
	if b < vmin {
		vmin = b
	}
	diff := v - vmin
	s = diff / (absf(v) + fltEpsilon)
	d := float32(60.0) / (diff + fltEpsilon)
	switch {
	case v == r:
		h = (g - b) * d
	case v == g:
		h = (b-r)*d + 120.0
	default:
		h = (r-g)*d + 240.0
	}
	if h < 0 {
		h += 360.0
	}
	return
}

// sectorData maps HSV sector → output channel selection (OpenCV order for RGB).
var sectorData = [6][3]int{{1, 3, 0}, {1, 0, 2}, {3, 0, 1}, {0, 2, 1}, {0, 1, 3}, {2, 1, 0}}

// hsv2rgbF replicates OpenCV's HSV2RGB_f. h in [0,360), s,v in [0,1] → RGB [0,1].
func hsv2rgbF(h, s, v float32) (float32, float32, float32) {
	if s == 0 {
		return v, v, v
	}
	hh := h * (1.0 / 60.0)
	for hh < 0 {
		hh += 6
	}
	for hh >= 6 {
		hh -= 6
	}
	sector := int(math.Floor(float64(hh)))
	hh -= float32(sector)
	if sector < 0 || sector >= 6 {
		sector = 0
		hh = 0
	}
	var tab [4]float32
	tab[0] = v
	tab[1] = v * (1 - s)
	tab[2] = v * (1 - s*hh)
	tab[3] = v * (1 - s*(1-hh))
	b := tab[sectorData[sector][0]]
	g := tab[sectorData[sector][1]]
	r := tab[sectorData[sector][2]]
	return r, g, b
}

func absf(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func fmod360(x float32) float32 {
	x = float32(math.Mod(float64(x), 360.0))
	if x < 0 {
		x += 360
	}
	return x
}

func searchsortedRight(a []float64, v float64) int {
	lo, hi := 0, len(a)
	for lo < hi {
		mid := (lo + hi) / 2
		if a[mid] <= v {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}
