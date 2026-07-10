// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Package adjust implements FreeCCR's color-correction chain (adjust_image,
// ccr_processor.py:2312), ported faithfully to a fused per-pixel Go pipeline.
//
// Every stage in adjust_image is per-pixel independent (no neighborhood), so the
// whole chain fuses into a single row-parallel pass: one read + one write of the
// buffer instead of numpy's ~10 separate full-array passes. Intermediate values
// stay in float32 (no uint16 truncation between stages) and are quantized only
// at the end, matching numpy.
package adjust

import "math"

// Params holds all adjust_image sliders. All values are in [-100,100] (some
// wider, e.g. exposure ±200), 0 = no change. Field order/names mirror the Python
// keyword args.
type Params struct {
	Kelvin, Tint  float64
	TintBalance   float64 // multiplier, default 1.0
	Exposure      float64
	Brightness    float64
	Blackpoint    float64
	Whitepoint    float64
	Contrast      float64
	Saturation    float64
	Highlights    float64
	Shadows       float64
	SubSaturation float64
	ChInputGain   float64
	ChMasterShift float64
	ChMasterGain  float64
	ChRShift      float64
	ChRGain       float64
	ChRBlackpoint float64
	ChGShift      float64
	ChGGain       float64
	ChGBlackpoint float64
	ChBShift      float64
	ChBGain       float64
	ChBBlackpoint float64
	// Bands, Curves, Gamma are applied by dedicated functions (see bands.go etc).
}

// DefaultParams returns a neutral Params (TintBalance=1).
func DefaultParams() Params { return Params{TintBalance: 1.0} }

// White-balance constants (ccr_processor.py:1044-1046).
const (
	wbTempStrength = 0.40
	wbTintStrength = 0.26
)

// WhiteBalanceGains returns flat per-channel gains (gr, gg, gb) for the
// Temperature/Tint controls. Neutral (0,0) → (1,1,1). Mirrors
// _white_balance_gains (ccr_processor.py:1026). Gains apply to channels 0,1,2
// (the Python comment labels them R,G,B).
func WhiteBalanceGains(kelvin, tint, balance float64) (gr, gg, gb float64) {
	gr, gg, gb = 1.0, 1.0, 1.0
	if kelvin != 0.0 {
		s := (kelvin / 100.0) * wbTempStrength
		gr *= 1.0 + s
		gb *= 1.0 - s
	}
	if tint != 0.0 {
		t := math.Tanh(tint*0.02) * wbTintStrength * balance
		gg *= 1.0 - t
		gr *= 1.0 + 0.3*t
		gb *= 1.0 + 0.3*t
	}
	return gr, gg, gb
}

func clampf64(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
