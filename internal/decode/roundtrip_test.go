// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package decode_test

import (
	"path/filepath"
	"testing"

	"github.com/zhengli/freeccr-go/internal/decode"
	"github.com/zhengli/freeccr-go/internal/export"
	"github.com/zhengli/freeccr-go/internal/image"
)

// TestTIFF16RoundTrip validates the hand-rolled 16-bit TIFF writer against the
// decoder: exact channel values must survive write → read.
func TestTIFF16RoundTrip(t *testing.T) {
	w, h := 17, 11
	im := image.New(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			im.Pix[i] = float32((x * 3701) % 65536)      // R
			im.Pix[i+1] = float32((y * 5903) % 65536)    // G
			im.Pix[i+2] = float32((x * y * 131) % 65536) // B
		}
	}
	path := filepath.Join(t.TempDir(), "rt.tif")
	if err := export.WriteTIFF16(path, im); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := decode.DecodeStandard(path)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.W != w || got.H != h {
		t.Fatalf("dims got %dx%d want %dx%d", got.W, got.H, w, h)
	}
	for i := range im.Pix {
		if got.Pix[i] != im.Pix[i] {
			t.Fatalf("idx %d got %v want %v", i, got.Pix[i], im.Pix[i])
		}
	}
}
