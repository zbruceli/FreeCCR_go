// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package adjust

import (
	"math"

	"github.com/zhengli/freeccr-go/internal/convert"
	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// AdjustImage applies the full slider chain to a 16-bit image (integer-valued
// float32 in [0,65535]) in place-free fashion, returning a new quantized image.
// wsWindowed: when true, im is a windowed working-space base — the exposure /
// white-point / white-balance controls are consumed by a headroom-safe recovery
// pre-stage before the main chain. Mirrors adjust_image (ccr_processor.py:2312).
func AdjustImage(im *image.Image, p Params, wsWindowed bool) *image.Image {
	work := im
	if wsWindowed {
		work = workingSpaceRecovery(im, p)
		// exposure/whitepoint/kelvin/tint consumed by the recovery pre-stage.
		p.Exposure, p.Whitepoint, p.Kelvin, p.Tint = 0, 0, 0, 0
	}

	out := &image.Image{W: work.W, H: work.H, Pix: image.GetBuf(len(work.Pix))}

	// --- precompute per-stage coefficients + enable flags once ---
	c := newCoeffs(p)

	src := work.Pix
	dst := out.Pix
	par.Rows(work.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			i := y * work.W * 3
			end := i + work.W*3
			for ; i < end; i += 3 {
				r, g, b := src[i], src[i+1], src[i+2]
				r, g, b = c.pixel(r, g, b)
				dst[i] = quantU16(r)
				dst[i+1] = quantU16(g)
				dst[i+2] = quantU16(b)
			}
		}
	})
	if wsWindowed {
		image.PutBuf(work.Pix)
	}
	return out
}

// coeffs holds precomputed scalars and which stages are active.
type coeffs struct {
	wb                        bool
	gr, gg, gb                float32
	exposure                  bool
	expWhiteVal               float32
	brightness                bool
	brightCurve               float32
	hs                        bool
	hMul, sMul                float32 // (h/100)*STRENGTH etc
	bwp                       bool
	blackVal, whiteMinusBlack float32
	contrast                  bool
	k                         float32
	saturation                bool
	satScaleMinus1            float32
	subSat                    bool
	subGamma                  float32
	chLevels                  bool
	ig                        float32
	shift, blk, whiteV        [3]float32
	chActive                  [3]bool

	// Scalar-stage LUTs: WB, exposure, brightness, highlights/shadows,
	// black/white point and contrast are pure per-channel functions of a single
	// integer input, so they compose into one 65536-entry LUT per channel —
	// built once, reused for every pixel (and every frame of a roll).
	// Bit-identical to evaluating the stage chain per pixel.
	scalarActive bool
	gain         [3]float32
	lut          [3][]float32
}

const (
	hsPeak     = float32(0.10546875)
	hsStrength = float32(0.30)
)

func newCoeffs(p Params) coeffs {
	var c coeffs
	if p.Kelvin != 0 || p.Tint != 0 {
		gr, gg, gb := WhiteBalanceGains(p.Kelvin, p.Tint, p.TintBalance)
		c.wb, c.gr, c.gg, c.gb = true, float32(gr), float32(gg), float32(gb)
	}
	if p.Exposure != 0 {
		gm := clampf64(p.Exposure, -200, 200) / 300.0
		c.exposure, c.expWhiteVal = true, float32(1.0-gm)
	}
	if p.Brightness != 0 {
		c.brightness = true
		c.brightCurve = float32(1.0 - 0.3*(p.Brightness/8.0))
	}
	if p.Highlights != 0 || p.Shadows != 0 {
		c.hs = true
		c.hMul = float32(p.Highlights/100.0) * hsStrength
		c.sMul = float32(p.Shadows/100.0) * hsStrength
	}
	if p.Blackpoint != 0 || p.Whitepoint != 0 {
		bc := clampf64(p.Blackpoint, -100, 100) / 300.0
		wc := clampf64(p.Whitepoint, -100, 100) / 300.0
		bv := 0.0 + bc
		wv := 1.0 - wc
		c.bwp, c.blackVal, c.whiteMinusBlack = true, float32(bv), float32(wv-bv)
	}
	if p.Contrast != 0 {
		c.contrast = true
		c.k = float32(clampf64(p.Contrast/105.0, -0.95, 0.95))
	}
	if p.Saturation != 0 {
		c.saturation = true
		c.satScaleMinus1 = float32(p.Saturation / 100.0) // (1+sat/100)-1
	}
	if p.SubSaturation != 0 {
		c.subSat = true
		c.subGamma = float32(math.Exp2(p.SubSaturation / 100.0))
	}
	// Per-channel levels.
	ig := math.Exp2(p.ChInputGain / 50.0)
	shifts := [3]float64{
		clampf64(p.ChMasterShift+p.ChRShift, -100, 100) / 300.0,
		clampf64(p.ChMasterShift+p.ChGShift, -100, 100) / 300.0,
		clampf64(p.ChMasterShift+p.ChBShift, -100, 100) / 300.0,
	}
	gains := [3]float64{
		clampf64(p.ChMasterGain+p.ChRGain, -100, 100) / 300.0,
		clampf64(p.ChMasterGain+p.ChGGain, -100, 100) / 300.0,
		clampf64(p.ChMasterGain+p.ChBGain, -100, 100) / 300.0,
	}
	blacks := [3]float64{
		clampf64(p.ChRBlackpoint, -100, 100) / 300.0,
		clampf64(p.ChGBlackpoint, -100, 100) / 300.0,
		clampf64(p.ChBBlackpoint, -100, 100) / 300.0,
	}
	anyCh := false
	for k := 0; k < 3; k++ {
		active := !(ig == 1.0 && shifts[k] == 0.0 && gains[k] == 0.0 && blacks[k] == 0.0)
		c.chActive[k] = active
		c.shift[k] = float32(shifts[k])
		c.blk[k] = float32(blacks[k])
		c.whiteV[k] = float32(1.0 - gains[k])
		if active {
			anyCh = true
		}
	}
	c.ig = float32(ig)
	c.chLevels = anyCh

	// Compose the pre-saturation scalar stages into per-channel LUTs.
	c.gain = [3]float32{1, 1, 1}
	if c.wb {
		c.gain = [3]float32{c.gr, c.gg, c.gb}
	}
	c.scalarActive = c.wb || c.exposure || c.brightness || c.hs || c.bwp || c.contrast
	if c.scalarActive {
		for ch := 0; ch < 3; ch++ {
			t := make([]float32, 65536)
			for v := 0; v < 65536; v++ {
				t[v] = c.scalarChannel(float32(v), ch)
			}
			c.lut[ch] = t
		}
	}
	return c
}

// scalarChannel applies the pre-saturation scalar stages (WB gain, exposure,
// brightness, highlights/shadows, black/white point, contrast) to one channel
// value in [0,65535]. Used to build the per-channel LUT; the ops mirror the
// former inline per-pixel code exactly, so the LUT is bit-identical.
func (c *coeffs) scalarChannel(v float32, ch int) float32 {
	const inv = float32(1.0 / 65535.0)
	if c.wb {
		v *= c.gain[ch]
	}
	if c.exposure {
		v = clamp01f(v*inv/c.expWhiteVal) * 65535
	}
	if c.brightness {
		v = powClamp01(v*inv, c.brightCurve) * 65535
	}
	if c.hs {
		v = c.hsChan(v * inv)
	}
	if c.bwp {
		v = clamp01f((v*inv-c.blackVal)/c.whiteMinusBlack) * 65535
	}
	if c.contrast {
		v = sCurve(v*inv, c.k) * 65535
	}
	return v
}

// pixel applies the full chain to one pixel (channel values in [0,65535]).
func (c *coeffs) pixel(r, g, b float32) (float32, float32, float32) {
	const inv = float32(1.0 / 65535.0)

	// Pre-saturation scalar stages collapse to one lookup per channel. Inputs
	// are integer-valued in [0,65535] (quantized by the convert stage).
	if c.scalarActive {
		r = c.lut[0][lutIdx(r)]
		g = c.lut[1][lutIdx(g)]
		b = c.lut[2][lutIdx(b)]
	}
	if c.saturation {
		rn, gn, bn := r*inv, g*inv, b*inv
		gray := lumW0*rn + lumW1*gn + lumW2*bn
		w := gray - 0.50
		midHigh := float32(math.Exp(float64(-(w / 0.35) * (w / 0.35))))
		satCurve := 0.2 + 0.8*midHigh
		dyn := 1.0 + c.satScaleMinus1*satCurve
		rn = clamp01f(gray + dyn*(rn-gray))
		gn = clamp01f(gray + dyn*(gn-gray))
		bn = clamp01f(gray + dyn*(bn-gray))
		r, g, b = rn*65535, gn*65535, bn*65535
	}
	if c.subSat {
		rn := clamp01f(r * inv)
		gn := clamp01f(g * inv)
		bn := clamp01f(b * inv)
		mx := rn
		if gn > mx {
			mx = gn
		}
		if bn > mx {
			mx = bn
		}
		if mx > 1e-6 {
			rn = mx * powf(rn/mx, c.subGamma)
			gn = mx * powf(gn/mx, c.subGamma)
			bn = mx * powf(bn/mx, c.subGamma)
		}
		r, g, b = rn*65535, gn*65535, bn*65535
	}
	if c.chLevels {
		r = c.chChan(0, r*inv)
		g = c.chChan(1, g*inv)
		b = c.chChan(2, b*inv)
	}
	return r, g, b
}

// hsChan applies the highlights/shadows bump to one normalized channel value.
func (c *coeffs) hsChan(x float32) float32 {
	om := 1.0 - x
	wh := x * x * x * om / hsPeak
	ws := x * om * om * om / hsPeak
	x = x + c.hMul*wh + c.sMul*ws
	return clamp01f(x) * 65535
}

// chChan applies the per-channel-levels stage to channel k (normalized value).
func (c *coeffs) chChan(k int, ch float32) float32 {
	if !c.chActive[k] {
		return ch * 65535
	}
	if c.ig != 1.0 {
		ch *= c.ig
	}
	if c.shift[k] != 0.0 {
		ch += c.shift[k]
	}
	bv, wv := c.blk[k], c.whiteV[k]
	if bv != 0.0 || wv != 1.0 {
		ch = (ch - bv) / (wv - bv)
	}
	return clamp01f(ch) * 65535
}

// lutIdx clamps a channel value to a valid LUT index. Inputs are already
// integer-valued in [0,65535]; the clamp is defensive against any stray range.
func lutIdx(v float32) int {
	if v <= 0 {
		return 0
	}
	if v >= 65535 {
		return 65535
	}
	return int(v)
}

func sCurve(x, k float32) float32 {
	d := x - 0.5
	ad := d
	if ad < 0 {
		ad = -ad
	}
	return ((1+k)*d)/(1+k*ad*2) + 0.5
}

// workingSpaceRecovery de-windows a windowed base, applies WB + white-point +
// exposure recovery un-clamped, then clamps into the display window and returns
// a floored uint16-valued image. Mirrors _apply_working_space_recovery
// (ccr_processor.py:1045).
func workingSpaceRecovery(im *image.Image, p Params) *image.Image {
	out := &image.Image{W: im.W, H: im.H, Pix: image.GetBuf(len(im.Pix))}
	wsB := float32(convert.WsB)
	invWidth := float32(1.0 / (convert.WsW - convert.WsB))

	var gr, gg, gb float32 = 1, 1, 1
	wb := p.Kelvin != 0 || p.Tint != 0
	if wb {
		grd, ggd, gbd := WhiteBalanceGains(p.Kelvin, p.Tint, p.TintBalance)
		gr, gg, gb = float32(grd), float32(ggd), float32(gbd)
	}
	var wpMul float32 = 1
	if p.Whitepoint != 0 {
		wp := clampf64(p.Whitepoint, -100, 100)
		wpMul = float32(math.Exp2(convert.WsHeadroomStops * wp / 100.0))
	}
	var expMul float32 = 1
	if p.Exposure != 0 {
		whiteVal := 1.0 - clampf64(p.Exposure, -200, 200)/300.0
		expMul = float32(1.0 / whiteVal)
	}

	src := im.Pix
	dst := out.Pix
	par.Rows(im.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			i := y * im.W * 3
			end := i + im.W*3
			for ; i < end; i += 3 {
				d0 := (src[i] - wsB) * invWidth
				d1 := (src[i+1] - wsB) * invWidth
				d2 := (src[i+2] - wsB) * invWidth
				if wb {
					d0 *= gr
					d1 *= gg
					d2 *= gb
				}
				if wpMul != 1 {
					d0 *= wpMul
					d1 *= wpMul
					d2 *= wpMul
				}
				if expMul != 1 {
					d0 *= expMul
					d1 *= expMul
					d2 *= expMul
				}
				dst[i] = quantU16(clamp01f(d0) * 65535)
				dst[i+1] = quantU16(clamp01f(d1) * 65535)
				dst[i+2] = quantU16(clamp01f(d2) * 65535)
			}
		}
	})
	return out
}

// --- small helpers ---

const (
	lumW0 = float32(0.299)
	lumW1 = float32(0.587)
	lumW2 = float32(0.114)
)

func clamp01f(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func powf(x, e float32) float32 { return float32(math.Pow(float64(x), float64(e))) }
func powClamp01(x, e float32) float32 {
	return clamp01f(powf(x, e))
}

// quantU16 = np.clip(x,0,65535).astype(uint16): clamp then truncate.
func quantU16(x float32) float32 {
	if x <= 0 {
		return 0
	}
	if x >= 65535 {
		return 65535
	}
	return float32(math.Floor(float64(x)))
}
