// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Package image defines FreeCCR-go's working image type: a flat, interleaved
// float32 buffer with three channels per pixel.
//
// Channel order is treated positionally (channel 0, 1, 2) to mirror the Python
// reference, which operates on img[..., c] without semantic labels. Decoders are
// responsible for filling channels in the same order the Python decode produced
// (cv2 → BGR, rawpy → RGB); every convert/adjust kernel then applies identical
// indexed math, so output matches regardless of the conventional meaning.
package image

import "sync"

// Image is an H×W image with 3 interleaved float32 channels.
// Pix has length W*H*3; pixel (x,y) channel c is Pix[(y*W+x)*3+c].
// Values are nominally in [0, 65535] but kernels may transiently exceed that
// range (headroom) before a final clamp, matching the numpy float32 pipeline.
type Image struct {
	W, H int
	Pix  []float32
}

// New allocates a zeroed Image of the given dimensions.
func New(w, h int) *Image {
	return &Image{W: w, H: h, Pix: make([]float32, w*h*3)}
}

// Clone returns a deep copy of the image, drawing its buffer from the pool.
func (im *Image) Clone() *Image {
	out := &Image{W: im.W, H: im.H, Pix: GetBuf(len(im.Pix))}
	copy(out.Pix, im.Pix)
	return out
}

// Pixels returns the number of pixels (W*H).
func (im *Image) Pixels() int { return im.W * im.H }

// CropNormRect returns a copy of the sub-rectangle given in normalized [0,1]
// coordinates (x0,y0)-(x1,y1). Bounds are clamped and the crop is at least 1×1.
// The returned buffer comes from the pool.
func (im *Image) CropNormRect(x0, y0, x1, y1 float64) *Image {
	cx0 := clampi(int(x0*float64(im.W)+0.5), 0, im.W-1)
	cy0 := clampi(int(y0*float64(im.H)+0.5), 0, im.H-1)
	cx1 := clampi(int(x1*float64(im.W)+0.5), cx0+1, im.W)
	cy1 := clampi(int(y1*float64(im.H)+0.5), cy0+1, im.H)
	cw, ch := cx1-cx0, cy1-cy0
	out := &Image{W: cw, H: ch, Pix: GetBuf(cw * ch * 3)}
	for y := 0; y < ch; y++ {
		copy(out.Pix[y*cw*3:(y+1)*cw*3],
			im.Pix[((cy0+y)*im.W+cx0)*3:((cy0+y)*im.W+cx1)*3])
	}
	return out
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// --- buffer pool ---------------------------------------------------------
//
// Convert/adjust stages churn through same-sized float32 buffers (one per
// frame, reused across a roll). A size-bucketed sync.Pool keeps allocation and
// GC pressure off the hot path for batch processing.

var pools sync.Map // int(len) -> *sync.Pool

// GetBuf returns a float32 slice of exactly length n (len==cap==n), possibly
// reused. Contents are not zeroed — callers must overwrite before reading.
func GetBuf(n int) []float32 {
	if n == 0 {
		return nil
	}
	p := poolFor(n)
	if v := p.Get(); v != nil {
		return v.([]float32)
	}
	return make([]float32, n)
}

// PutBuf returns a buffer obtained from GetBuf to the pool.
func PutBuf(b []float32) {
	if cap(b) == 0 {
		return
	}
	b = b[:cap(b)]
	poolFor(len(b)).Put(b) //nolint:staticcheck // slice header reused intentionally
}

func poolFor(n int) *sync.Pool {
	if v, ok := pools.Load(n); ok {
		return v.(*sync.Pool)
	}
	p := &sync.Pool{}
	actual, _ := pools.LoadOrStore(n, p)
	return actual.(*sync.Pool)
}
