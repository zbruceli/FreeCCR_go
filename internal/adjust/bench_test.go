// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package adjust

import (
	"math/rand"
	"testing"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

func benchImage(w, h int) *image.Image {
	im := image.New(w, h)
	rng := rand.New(rand.NewSource(1))
	for i := range im.Pix {
		im.Pix[i] = float32(rng.Intn(65536))
	}
	return im
}

// comboParams exercises the full chain (every stage active).
func comboParams() Params {
	p := DefaultParams()
	p.Kelvin, p.Tint = 30, -20
	p.Exposure, p.Brightness = 40, 15
	p.Highlights, p.Shadows = 30, 25
	p.Blackpoint, p.Whitepoint, p.Contrast = 20, -15, 35
	p.Saturation, p.SubSaturation = 40, 25
	p.ChInputGain, p.ChMasterGain, p.ChRGain = 10, 8, 12
	return p
}

// BenchmarkAdjustFull times the full fused chain at 2000x1333 (~2.6 MP).
func BenchmarkAdjustFull(b *testing.B) {
	im := benchImage(2000, 1333)
	p := comboParams()
	b.SetBytes(int64(len(im.Pix) * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := AdjustImage(im, p, false)
		image.PutBuf(out.Pix)
	}
}

// BenchmarkAdjustFull1 is the same, forced single-threaded (numpy-comparable).
func BenchmarkAdjustFull1(b *testing.B) {
	prev := par.MaxWorkers()
	par.SetMaxWorkers(1)
	defer par.SetMaxWorkers(prev)
	im := benchImage(2000, 1333)
	p := comboParams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := AdjustImage(im, p, false)
		image.PutBuf(out.Pix)
	}
}
