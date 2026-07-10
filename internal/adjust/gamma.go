// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package adjust

import (
	"math"
	"sync"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// gammaMaxOffset is the perpendicular offset (0..255 domain) at slider ±100.
const gammaMaxOffset = 63.75

// gammaCurvePoints returns the 3-point composite control-point list for a Gamma
// slider value. gamma=0 → identity. Mirrors gamma_curve_points.
func gammaCurvePoints(gamma float64) [][2]float64 {
	off := (gamma / 100.0) * gammaMaxOffset
	return [][2]float64{{0, 0}, {127.5 - off, 127.5 + off}, {255, 255}}
}

// ApplyGamma applies the Gamma slider as a center-point tone curve. No-op at 0.
// luminance=false applies it per channel (via ApplyCurves); luminance=true
// applies it to luminance and scales RGB together (hue-preserving). Mirrors
// apply_gamma_curve (ccr_processor.py:3145).
func ApplyGamma(im *image.Image, gamma float64, luminance bool) *image.Image {
	if gamma == 0 {
		return im.Clone()
	}
	if luminance {
		return applyGammaLuminance(im, gamma)
	}
	return ApplyCurves(im, &Curves{RGB: gammaCurvePoints(gamma)})
}

// gammaLUT16 builds the 65536→uint16 gamma tone LUT the same way apply_curves
// builds the composite path (256-level rint then expand). Mirrors _gamma_lut16.
func gammaLUT16(gamma float64) []uint16 {
	c256 := buildChannelLUT(gammaCurvePoints(gamma))
	for i := range c256 {
		c256[i] = float32(math.RoundToEven(float64(c256[i])))
	}
	return expandCurve256ToLUT16(c256)
}

func applyGammaLuminance(im *image.Image, gamma float64) *image.Image {
	lut := gammaLUT16(gamma)
	out := newLikeA(im)
	src, dst := im.Pix, out.Pix
	par.Rows(im.H, func(lo, hi int) {
		for i := lo * im.W * 3; i < hi*im.W*3; i += 3 {
			r, g, b := src[i], src[i+1], src[i+2]
			lum := lumW0*r + lumW1*g + lumW2*b
			idx := int(math.RoundToEven(float64(lum)))
			if idx < 0 {
				idx = 0
			} else if idx > 65535 {
				idx = 65535
			}
			lumOut := float32(lut[idx])
			denom := lum
			if denom < 1.0 {
				denom = 1.0
			}
			k := lumOut / denom
			dst[i] = rintClamp16(r * k)
			dst[i+1] = rintClamp16(g * k)
			dst[i+2] = rintClamp16(b * k)
		}
	})
	return out
}

// rintClamp16 = np.clip(np.rint(v),0,65535): round half-to-even then clamp.
func rintClamp16(v float32) float32 {
	r := math.RoundToEven(float64(v))
	if r < 0 {
		return 0
	}
	if r > 65535 {
		return 65535
	}
	return float32(r)
}

// --- Cineon log → Rec.709 (γ 2.2) -----------------------------------------

const (
	cineonBlackCode      = 95.0
	cineonWhiteCode      = 685.0
	cineonNegGamma       = 0.6
	cineonDensityPerCode = 0.002
)

var (
	cineonLUTOnce sync.Once
	cineonLUT     []uint16
)

func cineonRec709LUT16() []uint16 {
	cineonLUTOnce.Do(func() {
		cineonLUT = make([]uint16, 65536)
		gain := cineonDensityPerCode / cineonNegGamma
		off := math.Pow(10, (cineonBlackCode-cineonWhiteCode)*gain)
		for i := 0; i < 65536; i++ {
			code := float64(i) * (1023.0 / 65535.0)
			lin := (math.Pow(10, (code-cineonWhiteCode)*gain) - off) / (1.0 - off)
			if lin < 0 {
				lin = 0
			} else if lin > 1 {
				lin = 1
			}
			cineonLUT[i] = uint16(math.Round(math.Pow(lin, 1.0/2.2) * 65535.0))
		}
	})
	return cineonLUT
}

// ApplyCineon applies the Cineon log → Rec.709 transform per channel. Mirrors
// apply_cineon_to_rec709 (ccr_processor.py:3192).
func ApplyCineon(im *image.Image) *image.Image {
	lut := cineonRec709LUT16()
	out := newLikeA(im)
	src, dst := im.Pix, out.Pix
	par.Rows(im.H, func(lo, hi int) {
		for i := lo * im.W * 3; i < hi*im.W*3; i += 3 {
			dst[i] = float32(lut[lutIdx(src[i])])
			dst[i+1] = float32(lut[lutIdx(src[i+1])])
			dst[i+2] = float32(lut[lutIdx(src[i+2])])
		}
	})
	return out
}
