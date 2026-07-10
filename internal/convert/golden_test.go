// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package convert

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/zhengli/freeccr-go/internal/image"
)

type goldenCase struct {
	Kind    string    `json:"kind"`
	Density bool      `json:"density"`
	WS      bool      `json:"ws"`
	Black   []float64 `json:"black"`
	White   []float64 `json:"white"`
	Slopes  []float64 `json:"slopes"`
	W       int       `json:"w"`
	H       int       `json:"h"`
	Inp     []float64 `json:"inp"`
	Out     []int     `json:"out"`

	CropW     int       `json:"crop_w"`
	CropH     int       `json:"crop_h"`
	Crop      []float64 `json:"crop"`
	PLo       []float64 `json:"p_lo"`
	PHi       []float64 `json:"p_hi"`
	ODFactors []float64 `json:"od_factors"`
}

func loadGolden(t *testing.T) []goldenCase {
	t.Helper()
	b, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var cs []goldenCase
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}
	return cs
}

func imgFrom(w, h int, data []float64) *image.Image {
	im := image.New(w, h)
	for i, v := range data {
		im.Pix[i] = float32(v)
	}
	return im
}

// compare asserts every output channel is within tol LSB of the golden value.
func compare(t *testing.T, name string, got *image.Image, want []int, tol int) {
	t.Helper()
	if len(got.Pix) != len(want) {
		t.Fatalf("%s: len mismatch got %d want %d", name, len(got.Pix), len(want))
	}
	maxDiff, nBad := 0, 0
	for i := range want {
		d := int(got.Pix[i]) - want[i]
		if d < 0 {
			d = -d
		}
		if d > maxDiff {
			maxDiff = d
		}
		if d > tol {
			nBad++
			if nBad <= 6 {
				t.Errorf("%s: idx %d got %d want %d (diff %d)", name, i, int(got.Pix[i]), want[i], d)
			}
		}
	}
	if nBad > 0 {
		t.Errorf("%s: %d/%d channels exceed tol=%d (max diff %d)", name, nBad, len(want), tol, maxDiff)
	} else {
		t.Logf("%s: OK (max diff %d)", name, maxDiff)
	}
}

func to3(a []float64) [3]float64 { return [3]float64{a[0], a[1], a[2]} }

func TestGolden(t *testing.T) {
	cases := loadGolden(t)
	// Convert kernels use float64 transcendentals vs numpy float32; a 1-LSB
	// tolerance covers the rounding difference. Pure-arithmetic paths match
	// exactly (verified by max diff logged at 0).
	const tol = 1
	for i, c := range cases {
		switch c.Kind {
		case "twopoint":
			in := imgFrom(c.W, c.H, c.Inp)
			got := TwoPointInvert(in, to3(c.Black), to3(c.White), c.Density, c.WS)
			compare(t, name("twopoint", i, c), got, c.Out, tol)
		case "defslope":
			in := imgFrom(c.W, c.H, c.Inp)
			var slopes *[3]float64
			if c.Slopes != nil {
				s := to3(c.Slopes)
				slopes = &s
			}
			got := DefaultSlopeInvert(in, to3(c.Black), slopes, c.WS)
			compare(t, name("defslope", i, c), got, c.Out, tol)
		case "look":
			in := imgFrom(c.W, c.H, c.Inp)
			got := PostInvertLook(in)
			compare(t, name("look", i, c), got, c.Out, tol)
		case "refparams":
			crop := imgFrom(c.CropW, c.CropH, c.Crop)
			p := ComputeReferenceNormParams(crop)
			checkParams(t, i, p, c)
		case "refapply":
			in := imgFrom(c.W, c.H, c.Inp)
			p := RefParams{PLo: to3(c.PLo), PHi: to3(c.PHi), ODFactors: to3(c.ODFactors)}
			got := ApplyReferenceNormalization(in, p)
			compare(t, name("refapply", i, c), got, c.Out, tol)
		case "reffull":
			crop := imgFrom(c.CropW, c.CropH, c.Crop)
			p := ComputeReferenceNormParams(crop)
			in := imgFrom(c.W, c.H, c.Inp)
			got := ApplyReferenceNormalization(in, p)
			compare(t, name("reffull", i, c), got, c.Out, tol)
		default:
			t.Fatalf("case %d: unknown kind %q", i, c.Kind)
		}
	}
}

// checkParams compares against numpy's float32-computed params with a relative
// tolerance sized for float32 precision (numpy does percentile/mean in float32,
// Go in float64). The uint16-output impact is verified separately by "reffull".
func checkParams(t *testing.T, i int, p RefParams, c goldenCase) {
	t.Helper()
	rel := func(name string, k int, got, want float64) {
		d := math.Abs(got - want)
		if d > 1e-4*math.Max(1, math.Abs(want)) {
			t.Errorf("refparams[%d] %s[%d] got %v want %v (diff %v)", i, name, k, got, want, d)
		}
	}
	for k := 0; k < 3; k++ {
		rel("p_lo", k, p.PLo[k], c.PLo[k])
		rel("p_hi", k, p.PHi[k], c.PHi[k])
		rel("od", k, p.ODFactors[k], c.ODFactors[k])
	}
}

func name(kind string, i int, c goldenCase) string {
	s := kind
	if c.Density {
		s += "/density"
	}
	if c.WS {
		s += "/ws"
	}
	return s
}
