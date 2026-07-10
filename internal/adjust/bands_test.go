// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package adjust

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/zhengli/freeccr-go/internal/image"
)

type bandCase struct {
	Name    string        `json:"name"`
	W       int           `json:"w"`
	H       int           `json:"h"`
	Bands   [7][4]float64 `json:"bands"`
	Feather float64       `json:"feather"`
	Inp     []int         `json:"inp"`
	Out     []int         `json:"out"`
}

// TestBandsGolden checks ApplyColorBands against the REAL cv2-based
// apply_color_band_adjustments (feather=0). Regenerate the fixtures with
// `/tmp/ccrvenv/bin/python ref/gen_bands.py <FreeCCR/src>`.
func TestBandsGolden(t *testing.T) {
	b, err := os.ReadFile("testdata/golden_bands.json")
	if err != nil {
		t.Skip("no band fixtures; run ref/gen_bands.py in the cv2 venv")
	}
	var cs []bandCase
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// HSV round-trip + float64-in-numpy vs float32-in-Go lookups; a couple LSB
	// of headroom covers the cv2/Go transcendental + dtype differences.
	const tol = 2
	for _, c := range cs {
		in := image.New(c.W, c.H)
		for j, v := range c.Inp {
			in.Pix[j] = float32(v)
		}
		got := ApplyColorBands(in, &BandSettings{Bands: c.Bands, Feather: c.Feather})
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
			}
		}
		if nBad > 0 {
			t.Errorf("band %q: %d channels exceed %d LSB (max %d)", c.Name, nBad, tol, maxDiff)
		} else {
			t.Logf("band %q: OK (max diff %d)", c.Name, maxDiff)
		}
	}
}
