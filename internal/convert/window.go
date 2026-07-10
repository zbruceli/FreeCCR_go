// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Package convert implements FreeCCR's negative→positive conversion kernels,
// ported from src/core/ccr_processor.py. Math is kept faithful to the numpy
// reference (float32 working values, floor-truncation at uint16 boundaries).
package convert

import (
	"math"

	"github.com/zhengli/freeccr-go/internal/image"
)

// Working-space window geometry (ccr_processor.py:988-1006). Defaults mirror
// FREECCR_WS_BITS=10, FREECCR_WS_LO=0.5.
const (
	wsBits     = 10
	wsLo       = 0.5
	wsWidth    = float64(1 << wsBits) // 1024
	WsB        = wsLo * wsWidth       // 512  — code for display-black (d=0)
	WsW        = (1.0 + wsLo) * wsWidth
	wsInvWidth = 1.0 / (WsW - WsB) // 1/1024
)

// WsHeadroomStops = log2((65535 - WsB) / (WsW - WsB)).
var WsHeadroomStops = math.Log2((65535.0 - WsB) / (WsW - WsB))

// Inversion / density constants (ccr_processor.py:972-977).
const (
	defaultDensitySlope = 0.8
	defaultDensityGamma = 1.0
	densityFloor        = 1.0
)

// quantU16 reproduces numpy's np.clip(x,0,65535).astype(np.uint16): clamp to
// the display range then truncate toward zero (floor for non-negatives).
func quantU16(x float32) float32 {
	if x <= 0 {
		return 0
	}
	if x >= 65535 {
		return 65535
	}
	return float32(math.Floor(float64(x)))
}

// clip01 clamps to [0,1].
func clip01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

// encodeWindow maps a display value d (0=black, 1=white, may overshoot) into a
// windowed uint16 container code, clamped to [0,65535] and truncated.
// Mirrors encode_window (ccr_processor.py:1008).
func encodeWindow(d float64) float32 {
	code := d*(WsW-WsB) + WsB
	if code <= 0 {
		return 0
	}
	if code >= 65535 {
		return 65535
	}
	return float32(math.Floor(code))
}

// newLike returns a fresh pooled image matching im's dimensions.
func newLike(im *image.Image) *image.Image {
	return &image.Image{W: im.W, H: im.H, Pix: image.GetBuf(len(im.Pix))}
}
