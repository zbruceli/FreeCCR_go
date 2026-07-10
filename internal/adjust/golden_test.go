// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package adjust

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/zhengli/freeccr-go/internal/image"
)

type adjCase struct {
	W      int                `json:"w"`
	H      int                `json:"h"`
	WS     bool               `json:"ws"`
	Params map[string]float64 `json:"params"`
	Inp    []int              `json:"inp"`
	Out    []int              `json:"out"`
}

// paramsFrom maps the Python keyword names to the Go Params struct.
func paramsFrom(m map[string]float64) Params {
	p := DefaultParams()
	set := map[string]*float64{
		"kelvin_shift": &p.Kelvin, "tint_shift": &p.Tint,
		"exposure": &p.Exposure, "brightness": &p.Brightness,
		"blackpoint": &p.Blackpoint, "whitepoint": &p.Whitepoint,
		"contrast": &p.Contrast, "saturation": &p.Saturation,
		"highlights": &p.Highlights, "shadows": &p.Shadows,
		"sub_saturation":  &p.SubSaturation,
		"ch_input_gain":   &p.ChInputGain,
		"ch_master_shift": &p.ChMasterShift, "ch_master_gain": &p.ChMasterGain,
		"ch_r_shift": &p.ChRShift, "ch_r_gain": &p.ChRGain, "ch_r_blackpoint": &p.ChRBlackpoint,
		"ch_g_shift": &p.ChGShift, "ch_g_gain": &p.ChGGain, "ch_g_blackpoint": &p.ChGBlackpoint,
		"ch_b_shift": &p.ChBShift, "ch_b_gain": &p.ChBGain, "ch_b_blackpoint": &p.ChBBlackpoint,
		"tint_balance_factor": &p.TintBalance,
	}
	for k, v := range m {
		if ptr, ok := set[k]; ok {
			*ptr = v
		} else {
			panic("unknown param " + k)
		}
	}
	return p
}

func TestAdjustGolden(t *testing.T) {
	b, err := os.ReadFile("testdata/golden_adjust.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var cs []adjCase
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	const tol = 1
	for i, c := range cs {
		in := image.New(c.W, c.H)
		for j, v := range c.Inp {
			in.Pix[j] = float32(v)
		}
		got := AdjustImage(in, paramsFrom(c.Params), c.WS)
		maxDiff, nBad := 0, 0
		for j := range c.Out {
			d := int(got.Pix[j]) - c.Out[j]
			if d < 0 {
				d = -d
			}
			if d > maxDiff {
				maxDiff = d
			}
			if d > tol {
				nBad++
				if nBad <= 4 {
					t.Errorf("case %d %v: idx %d got %d want %d (diff %d)", i, c.Params, j, int(got.Pix[j]), c.Out[j], d)
				}
			}
		}
		if nBad > 0 {
			t.Errorf("case %d %v: %d channels exceed tol (max %d)", i, c.Params, nBad, maxDiff)
		} else {
			t.Logf("case %d %v: OK (max diff %d)", i, c.Params, maxDiff)
		}
	}
}
