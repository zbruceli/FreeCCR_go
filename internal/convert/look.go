// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package convert

import (
	"math"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// Luminance weights applied to channels 0,1,2 in stored order
// (_LUM_WEIGHTS, ccr_processor.py:1734).
const (
	lumW0 = float32(0.299)
	lumW1 = float32(0.587)
	lumW2 = float32(0.114)
)

// PostInvertLook applies the shared saturation-boost + shadow-warmth styling.
// Input is a quantized inverted image (integer-valued float32 in [0,65535]);
// output is quantized uint16-valued float32. Mirrors apply_postinvert_look
// (ccr_processor.py:1737). Computed in float32 to track the numpy pipeline.
func PostInvertLook(im *image.Image) *image.Image {
	out := newLike(im)
	px := im.Pix
	op := out.Pix
	par.Rows(im.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			i := y * im.W * 3
			end := i + im.W*3
			for ; i < end; i += 3 {
				o0, o1, o2 := postInvertPixel(px[i], px[i+1], px[i+2])
				op[i] = o0
				op[i+1] = o1
				op[i+2] = o2
			}
		}
	})
	return out
}

// postInvertPixel runs the look on a single inverted pixel (channel values in
// [0,65535]) and returns the quantized result.
func postInvertPixel(v0, v1, v2 float32) (float32, float32, float32) {
	const inv = float32(1.0 / 65535.0)
	x0 := clamp01f(v0 * inv)
	x1 := clamp01f(v1 * inv)
	x2 := clamp01f(v2 * inv)

	// Saturation boost: dynamic = 1 + 0.15 * lum^0.8, applied around luminance.
	lum := lumW0*x0 + lumW1*x1 + lumW2*x2
	satCurve := float32(math.Pow(float64(lum), 0.8))
	dyn := float32(0.15)*satCurve + 1.0
	x0 = clamp01f((x0-lum)*dyn + lum)
	x1 = clamp01f((x1-lum)*dyn + lum)
	x2 = clamp01f((x2-lum)*dyn + lum)

	// Shadow warmth: recompute luminance, warm/green lift in the shadows.
	slum := lumW0*x0 + lumW1*x1 + lumW2*x2
	warmth := float32(math.Exp(float64(slum*-4.0))) * 0.35
	green := float32(math.Exp(float64(slum*-3.5))) * 0.15
	x0 *= 1.0 + warmth*0.8
	x1 *= 1.0 + green
	x2 *= 1.0 - warmth

	return quantU16(x0 * 65535.0), quantU16(x1 * 65535.0), quantU16(x2 * 65535.0)
}

func clamp01f(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
