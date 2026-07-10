// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package adjust

import (
	"math"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// Curves holds Photoshop-style tone-curve control points per channel, in the
// 0..255 domain. Empty/identity channels are no-ops. The composite "RGB" curve
// is applied first, then the per-channel curve — matching apply_curves
// (ccr_processor.py:3067).
type Curves struct {
	RGB [][2]float64
	R   [][2]float64
	G   [][2]float64
	B   [][2]float64
}

// IsIdentity reports whether every channel is identity/empty.
func (c *Curves) IsIdentity() bool {
	if c == nil {
		return true
	}
	return normalizePoints(c.RGB) == nil && normalizePoints(c.R) == nil &&
		normalizePoints(c.G) == nil && normalizePoints(c.B) == nil
}

var identityPoints = [][2]float64{{0, 0}, {255, 255}}

// normalizePoints sanitizes a control-point list: finite pairs clamped to
// [0,255], sorted by x, duplicate-x dropped. Returns nil for an
// identity/degenerate set. Mirrors _normalize_points (ccr_processor.py:2951).
func normalizePoints(points [][2]float64) [][2]float64 {
	if len(points) == 0 {
		return nil
	}
	cleaned := make([][2]float64, 0, len(points))
	for _, p := range points {
		x, y := p[0], p[1]
		if math.IsNaN(x) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
			continue
		}
		cleaned = append(cleaned, [2]float64{clampf64(x, 0, 255), clampf64(y, 0, 255)})
	}
	if len(cleaned) < 2 {
		return nil
	}
	// stable sort by x
	for i := 1; i < len(cleaned); i++ {
		for j := i; j > 0 && cleaned[j-1][0] > cleaned[j][0]; j-- {
			cleaned[j-1], cleaned[j] = cleaned[j], cleaned[j-1]
		}
	}
	dedup := cleaned[:1]
	for _, q := range cleaned[1:] {
		if q[0] > dedup[len(dedup)-1][0] {
			dedup = append(dedup, q)
		}
	}
	if len(dedup) < 2 {
		return nil
	}
	if len(dedup) == 2 && dedup[0] == identityPoints[0] && dedup[1] == identityPoints[1] {
		return nil
	}
	return dedup
}

// monotoneCubic evaluates a Fritsch–Carlson monotone cubic through (xs,ys) at
// each xq. xs strictly increasing. Mirrors _monotone_cubic (ccr_processor.py:2992).
func monotoneCubic(xs, ys []float64, xq []float64) []float64 {
	n := len(xs)
	out := make([]float64, len(xq))
	if n == 2 {
		for i, x := range xq {
			out[i] = linInterp(x, xs, ys)
		}
		return out
	}
	h := make([]float64, n-1)
	delta := make([]float64, n-1)
	for i := 0; i < n-1; i++ {
		h[i] = xs[i+1] - xs[i]
		delta[i] = (ys[i+1] - ys[i]) / h[i]
	}
	m := make([]float64, n)
	for i := 1; i < n-1; i++ {
		m[i] = (delta[i-1] + delta[i]) / 2.0
	}
	m[0] = delta[0]
	m[n-1] = delta[n-2] // Python delta[-1] = last diff
	for i := 0; i < n-1; i++ {
		if delta[i] == 0.0 {
			m[i] = 0.0
			m[i+1] = 0.0
		} else {
			a := m[i] / delta[i]
			b := m[i+1] / delta[i]
			s := a*a + b*b
			if s > 9.0 {
				t := 3.0 / math.Sqrt(s)
				m[i] = t * a * delta[i]
				m[i+1] = t * b * delta[i]
			}
		}
	}
	for i, x := range xq {
		// segment index = clip(searchsorted(xs,x)-1, 0, n-2)
		idx := searchsorted(xs, x) - 1
		if idx < 0 {
			idx = 0
		}
		if idx > n-2 {
			idx = n - 2
		}
		x0, x1 := xs[idx], xs[idx+1]
		y0, y1 := ys[idx], ys[idx+1]
		m0, m1 := m[idx], m[idx+1]
		hh := x1 - x0
		tt := (x - x0) / hh
		t2 := tt * tt
		t3 := t2 * tt
		h00 := 2*t3 - 3*t2 + 1
		h10 := t3 - 2*t2 + tt
		h01 := -2*t3 + 3*t2
		h11 := t3 - t2
		out[i] = h00*y0 + h10*hh*m0 + h01*y1 + h11*hh*m1
	}
	return out
}

// buildChannelLUT builds a 256-entry (output 0..255) curve from control points.
// Identity/invalid → identity ramp. Mirrors build_channel_lut.
func buildChannelLUT(points [][2]float64) []float32 {
	lut := make([]float32, 256)
	pts := normalizePoints(points)
	if pts == nil {
		for i := range lut {
			lut[i] = float32(i)
		}
		return lut
	}
	xs := make([]float64, len(pts))
	ys := make([]float64, len(pts))
	for i, p := range pts {
		xs[i] = p[0]
		ys[i] = p[1]
	}
	xq := make([]float64, 256)
	for i := range xq {
		xq[i] = float64(i)
	}
	y := monotoneCubic(xs, ys, xq)
	for i := range lut {
		lut[i] = float32(clampf64(y[i], 0, 255))
	}
	return lut
}

// expandCurve256ToLUT16 expands a 256-entry (0..255) curve to a 65536→uint16
// LUT via linear interpolation. Mirrors _expand_curve256_to_lut16.
func expandCurve256ToLUT16(curve256 []float32) []uint16 {
	// src positions: i * 65535/255 for i in 0..255; values: curve256[i]*65535/255.
	const sc = 65535.0 / 255.0
	lut := make([]uint16, 65536)
	// Build scaled source arrays.
	srcX := make([]float64, 256)
	srcY := make([]float64, 256)
	for i := 0; i < 256; i++ {
		srcX[i] = float64(i) * sc
		srcY[i] = float64(curve256[i]) * sc
	}
	for v := 0; v < 65536; v++ {
		lut[v] = uint16(linInterp(float64(v), srcX, srcY))
	}
	return lut
}

// ApplyCurves applies the composite-then-per-channel tone curves to a 16-bit
// image (integer-valued float32). Returns a new image; if all channels are
// identity, returns a clone unchanged. Mirrors apply_curves (ccr_processor.py:3067).
func ApplyCurves(im *image.Image, c *Curves) *image.Image {
	if c.IsIdentity() {
		return im.Clone()
	}
	rgbLUT := buildChannelLUT(c.RGB) // 256 float
	rgbIdx := make([]int, 256)
	for i, v := range rgbLUT {
		r := int(math.RoundToEven(float64(v))) // np.rint
		if r < 0 {
			r = 0
		} else if r > 255 {
			r = 255
		}
		rgbIdx[i] = r
	}

	chPoints := [3][][2]float64{c.R, c.G, c.B}
	var lut16 [3][]uint16
	for ci := 0; ci < 3; ci++ {
		chLUT := buildChannelLUT(chPoints[ci])
		composed := make([]float32, 256)
		for i := 0; i < 256; i++ {
			composed[i] = chLUT[rgbIdx[i]]
		}
		lut16[ci] = expandCurve256ToLUT16(composed)
	}

	out := newLikeA(im)
	src, dst := im.Pix, out.Pix
	par.Rows(im.H, func(lo, hi int) {
		for i := lo * im.W * 3; i < hi*im.W*3; i += 3 {
			dst[i] = float32(lut16[0][lutIdx(src[i])])
			dst[i+1] = float32(lut16[1][lutIdx(src[i+1])])
			dst[i+2] = float32(lut16[2][lutIdx(src[i+2])])
		}
	})
	return out
}

// --- small numeric helpers ---

// linInterp mirrors np.interp for a single query point x over increasing xp,fp
// (clamped at the ends).
func linInterp(x float64, xp, fp []float64) float64 {
	n := len(xp)
	if x <= xp[0] {
		return fp[0]
	}
	if x >= xp[n-1] {
		return fp[n-1]
	}
	// binary search for the interval
	lo, hi := 0, n-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if xp[mid] <= x {
			lo = mid
		} else {
			hi = mid
		}
	}
	t := (x - xp[lo]) / (xp[hi] - xp[lo])
	return fp[lo] + t*(fp[hi]-fp[lo])
}

// searchsorted mirrors np.searchsorted(a, v) with side="left".
func searchsorted(a []float64, v float64) int {
	lo, hi := 0, len(a)
	for lo < hi {
		mid := (lo + hi) / 2
		if a[mid] < v {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

func newLikeA(im *image.Image) *image.Image {
	return &image.Image{W: im.W, H: im.H, Pix: image.GetBuf(len(im.Pix))}
}
