// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package convert

import (
	"math"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// TwoPointInvert performs the two-point B/W-point inversion → positive.
// black is the clear/film-base sample (HIGH scan value), white the dense/exposed
// sample (LOW value); both are per-channel in the image's own channel order.
// density=true recovers optical density in log space; false uses the legacy
// linear transmittance stretch. ws selects the working-space windowed output.
// Mirrors _twopoint_invert (ccr_processor.py:1213). No post-invert look is
// applied (the B/W-point path leaves it disabled).
func TwoPointInvert(im *image.Image, black, white [3]float64, density, ws bool) *image.Image {
	out := newLike(im)

	// Per-channel constants, computed once.
	var (
		base, dense, dmax, denom [3]float64
		degenerate               [3]bool // channel forced to a constant
		constVal                 [3]float32
	)
	for c := 0; c < 3; c++ {
		base[c] = math.Max(black[c], 1.0)
		dense[c] = math.Max(white[c], 1.0)
		if density {
			if base[c] > dense[c] {
				dmax[c] = math.Log10(base[c] / dense[c])
			}
			if dmax[c] <= 1e-6 {
				degenerate[c] = true
				constVal[c] = degenConst(ws, false) // → 0
			}
		} else {
			denom[c] = base[c] - dense[c]
			if math.Abs(denom[c]) < 1.0 {
				degenerate[c] = true
				// linear degenerate: ws→white(1.0 display), non-ws→0 pre-invert
				// which becomes 65535 after the final invert.
				if ws {
					constVal[c] = encodeWindow(1.0)
				} else {
					constVal[c] = 65535
				}
			}
		}
	}

	px := im.Pix
	op := out.Pix
	par.Rows(im.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			i := y * im.W * 3
			end := i + im.W*3
			for ; i < end; i += 3 {
				for c := 0; c < 3; c++ {
					if degenerate[c] {
						op[i+c] = constVal[c]
						continue
					}
					v := float64(px[i+c])
					if ws {
						var d float64
						if density {
							ch := math.Max(v, densityFloor)
							d = math.Log10(base[c]/ch) / dmax[c] // overshoot kept
						} else {
							t := (v - dense[c]) / denom[c]
							d = 1.0 - t
						}
						op[i+c] = encodeWindow(d)
					} else {
						if density {
							ch := math.Max(v, densityFloor)
							d := clip01(math.Log10(base[c]/ch) / dmax[c])
							op[i+c] = quantU16(float32(d * 65535.0))
						} else {
							n := (v - dense[c]) / denom[c] * 65535.0
							if n < 0 {
								n = 0
							} else if n > 65535 {
								n = 65535
							}
							op[i+c] = quantU16(float32(65535.0 - n))
						}
					}
				}
			}
		}
	})
	return out
}

// DefaultSlopeInvert performs the black-point-only density-space inversion.
// slopes, when non-nil, supplies per-channel film-stock density slopes;
// otherwise the scalar defaultDensitySlope is used for every channel.
// Mirrors _default_slope_invert (ccr_processor.py:1087).
func DefaultSlopeInvert(im *image.Image, black [3]float64, slopes *[3]float64, ws bool) *image.Image {
	out := newLike(im)

	var baseArr, slope [3]float64
	for c := 0; c < 3; c++ {
		baseArr[c] = math.Max(black[c], 1.0)
		if slopes == nil {
			slope[c] = defaultDensitySlope
		} else {
			slope[c] = slopes[c]
		}
	}
	applyGamma := defaultDensityGamma != 1.0
	invGamma := 1.0 / defaultDensityGamma

	px := im.Pix
	op := out.Pix
	par.Rows(im.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			i := y * im.W * 3
			end := i + im.W*3
			for ; i < end; i += 3 {
				for c := 0; c < 3; c++ {
					ch := math.Max(float64(px[i+c]), densityFloor)
					d := math.Log10(baseArr[c] / ch)
					if d < 0 {
						d = 0
					}
					d *= slope[c]
					if ws {
						if applyGamma {
							d = math.Pow(d, invGamma)
						}
						op[i+c] = encodeWindow(d)
					} else {
						d = clip01(d)
						if applyGamma {
							d = math.Pow(d, invGamma)
						}
						op[i+c] = quantU16(float32(d * 65535.0))
					}
				}
			}
		}
	})
	return out
}

// degenConst returns the constant a degenerate channel takes. density degenerate
// is 0 in both ws and non-ws (encode_window(0) and 0*65535 both floor to WsB and
// 0 respectively — but the Python sets d=0.0 pre-encode in ws, and norm=0
// pre-nothing in non-ws density). Handled explicitly by callers; kept for the
// density branch where ws→encodeWindow(0), non-ws→0.
func degenConst(ws, _ bool) float32 {
	if ws {
		return encodeWindow(0.0)
	}
	return 0
}
