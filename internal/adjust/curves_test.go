// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package adjust

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/zhengli/freeccr-go/internal/image"
)

type curveCase struct {
	Kind      string                  `json:"kind"`
	W         int                     `json:"w"`
	H         int                     `json:"h"`
	Inp       []int                   `json:"inp"`
	Curves    map[string][][2]float64 `json:"curves"`
	Gamma     float64                 `json:"gamma"`
	Luminance bool                    `json:"luminance"`
	Out       []int                   `json:"out"`
}

func TestCurvesGolden(t *testing.T) {
	b, err := os.ReadFile("testdata/golden_curves.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var cs []curveCase
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for i, c := range cs {
		in := image.New(c.W, c.H)
		for j, v := range c.Inp {
			in.Pix[j] = float32(v)
		}
		var got *image.Image
		switch c.Kind {
		case "curves":
			got = ApplyCurves(in, curvesFrom(c.Curves))
		case "gamma":
			got = ApplyGamma(in, c.Gamma, c.Luminance)
		case "cineon":
			got = ApplyCineon(in)
		default:
			t.Fatalf("case %d: unknown kind %q", i, c.Kind)
		}
		maxDiff, nBad := 0, 0
		for j := range c.Out {
			d := int(got.Pix[j]) - c.Out[j]
			if d < 0 {
				d = -d
			}
			if d > maxDiff {
				maxDiff = d
			}
			if d > 1 {
				nBad++
			}
		}
		label := c.Kind
		if c.Kind == "gamma" && c.Luminance {
			label = "gamma/lum"
		}
		if nBad > 0 {
			t.Errorf("case %d (%s): %d channels exceed 1 LSB (max %d)", i, label, nBad, maxDiff)
		} else {
			t.Logf("case %d (%s): OK (max diff %d)", i, label, maxDiff)
		}
	}
}

func curvesFrom(m map[string][][2]float64) *Curves {
	c := &Curves{}
	if m == nil {
		return c
	}
	c.RGB = m["rgb"]
	c.R = m["r"]
	c.G = m["g"]
	c.B = m["b"]
	return c
}
