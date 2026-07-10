// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package convert

import (
	"math/rand"
	"testing"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

func benchImage(w, h int) *image.Image {
	im := image.New(w, h)
	rng := rand.New(rand.NewSource(2))
	for i := range im.Pix {
		im.Pix[i] = float32(1 + rng.Intn(65535))
	}
	return im
}

var (
	benchBlack = [3]float64{61000, 58000, 52000}
	benchWhite = [3]float64{9150, 8700, 7800}
)

func BenchmarkTwoPointDensity(b *testing.B) {
	im := benchImage(2000, 1333)
	b.SetBytes(int64(len(im.Pix) * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := TwoPointInvert(im, benchBlack, benchWhite, true, true)
		image.PutBuf(out.Pix)
	}
}

func BenchmarkReferenceApply(b *testing.B) {
	im := benchImage(2000, 1333)
	p := RefParams{
		PLo: [3]float64{800, 900, 1000}, PHi: [3]float64{64000, 64200, 64500},
		ODFactors: [3]float64{1.0, 1.05, 0.95},
	}
	b.SetBytes(int64(len(im.Pix) * 4))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := ApplyReferenceNormalization(im, p)
		image.PutBuf(out.Pix)
	}
}

func BenchmarkReferenceApply1(b *testing.B) {
	prev := par.MaxWorkers()
	par.SetMaxWorkers(1)
	defer par.SetMaxWorkers(prev)
	im := benchImage(2000, 1333)
	p := RefParams{
		PLo: [3]float64{800, 900, 1000}, PHi: [3]float64{64000, 64200, 64500},
		ODFactors: [3]float64{1.0, 1.05, 0.95},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := ApplyReferenceNormalization(im, p)
		image.PutBuf(out.Pix)
	}
}
