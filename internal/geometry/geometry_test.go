// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package geometry

import (
	"testing"

	"github.com/zhengli/freeccr-go/internal/image"
)

func sample(w, h int) *image.Image {
	im := image.New(w, h)
	for i := range im.Pix {
		im.Pix[i] = float32(i % 1000)
	}
	return im
}

func equal(a, b *image.Image) bool {
	if a.W != b.W || a.H != b.H {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}

func TestRotate90Identity(t *testing.T) {
	im := sample(7, 5)
	// Four 90° turns == identity; one turn swaps dims.
	one := Rotate90CW(im, 1)
	if one.W != im.H || one.H != im.W {
		t.Fatalf("rot90 dims: got %dx%d want %dx%d", one.W, one.H, im.H, im.W)
	}
	if !equal(Rotate90CW(im, 4), im) {
		t.Error("rot90 ×4 != identity")
	}
	if !equal(Rotate90CW(im, 2), Rotate90CW(Rotate90CW(im, 1), 1)) {
		t.Error("rot90(2) != rot90(1)∘rot90(1)")
	}
}

func TestFlipInvolution(t *testing.T) {
	im := sample(6, 4)
	if !equal(FlipH(FlipH(im)), im) {
		t.Error("flipH twice != identity")
	}
	if !equal(FlipV(FlipV(im)), im) {
		t.Error("flipV twice != identity")
	}
}

func TestCropDims(t *testing.T) {
	im := sample(100, 80)
	c := Crop(im, [4]float64{0.1, 0.2, 0.6, 0.7})
	if c.W != 50 || c.H != 40 {
		t.Errorf("crop dims: got %dx%d want 50x40", c.W, c.H)
	}
	// top-left of the crop equals the source pixel at (10,16).
	si := (16*im.W + 10) * 3
	if c.Pix[0] != im.Pix[si] {
		t.Errorf("crop origin pixel mismatch: %v vs %v", c.Pix[0], im.Pix[si])
	}
}

func TestFineRotateExpands(t *testing.T) {
	im := sample(40, 30)
	r := FineRotate(im, 30)
	if r.W <= im.W || r.H <= im.H {
		t.Errorf("fine-rotate should expand canvas: got %dx%d from %dx%d", r.W, r.H, im.W, im.H)
	}
	if FineRotate(im, 0) != im {
		t.Error("fine-rotate 0 should return input")
	}
}
