// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Package geometry provides image orientation + crop ops (90° rotate, flips,
// fine straighten, axis-aligned crop). Aims for visual correctness, not
// cv2-bit-exact parity — geometry is not a color-fidelity path.
package geometry

import (
	"math"

	"github.com/zhengli/freeccr-go/internal/image"
)

// Rotate90CW rotates the image clockwise by 90°·times (times mod 4).
func Rotate90CW(im *image.Image, times int) *image.Image {
	times = ((times % 4) + 4) % 4
	out := im
	for i := 0; i < times; i++ {
		out = rot90cw(out)
	}
	return out
}

func rot90cw(im *image.Image) *image.Image {
	w, h := im.W, im.H
	out := image.New(h, w) // new dims: W=h, H=w
	nw := h
	for ny := 0; ny < w; ny++ {
		for nx := 0; nx < h; nx++ {
			ox, oy := ny, h-1-nx // inverse of CW map
			si := (oy*w + ox) * 3
			di := (ny*nw + nx) * 3
			out.Pix[di] = im.Pix[si]
			out.Pix[di+1] = im.Pix[si+1]
			out.Pix[di+2] = im.Pix[si+2]
		}
	}
	return out
}

// FlipH mirrors the image left↔right.
func FlipH(im *image.Image) *image.Image {
	w, h := im.W, im.H
	out := image.New(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			si := (y*w + (w - 1 - x)) * 3
			di := (y*w + x) * 3
			out.Pix[di] = im.Pix[si]
			out.Pix[di+1] = im.Pix[si+1]
			out.Pix[di+2] = im.Pix[si+2]
		}
	}
	return out
}

// FlipV mirrors the image top↔bottom.
func FlipV(im *image.Image) *image.Image {
	w, h := im.W, im.H
	out := image.New(w, h)
	for y := 0; y < h; y++ {
		src := (h - 1 - y) * w * 3
		copy(out.Pix[y*w*3:(y+1)*w*3], im.Pix[src:src+w*3])
	}
	return out
}

// Crop extracts the axis-aligned normalized rectangle (x0,y0,x1,y1 in [0,1]).
// Bounds are clamped; a degenerate rect returns the original.
func Crop(im *image.Image, rect [4]float64) *image.Image {
	x0 := clampi(int(rect[0]*float64(im.W)+0.5), 0, im.W-1)
	y0 := clampi(int(rect[1]*float64(im.H)+0.5), 0, im.H-1)
	x1 := clampi(int(rect[2]*float64(im.W)+0.5), x0+1, im.W)
	y1 := clampi(int(rect[3]*float64(im.H)+0.5), y0+1, im.H)
	cw, ch := x1-x0, y1-y0
	if cw <= 0 || ch <= 0 {
		return im
	}
	out := image.New(cw, ch)
	for y := 0; y < ch; y++ {
		src := ((y0+y)*im.W + x0) * 3
		copy(out.Pix[y*cw*3:(y+1)*cw*3], im.Pix[src:src+cw*3])
	}
	return out
}

// FineRotate rotates the image by deg degrees (clockwise) about its center onto
// an expanded canvas (bilinear, black outside). deg==0 returns the input.
func FineRotate(im *image.Image, deg float64) *image.Image {
	if deg == 0 {
		return im
	}
	rad := deg * math.Pi / 180.0
	c, s := math.Cos(rad), math.Sin(rad)
	w, h := float64(im.W), float64(im.H)
	nw := int(math.Ceil(math.Abs(w*c) + math.Abs(h*s)))
	nh := int(math.Ceil(math.Abs(w*s) + math.Abs(h*c)))
	out := image.New(nw, nh)
	cx, cy := w/2, h/2
	ncx, ncy := float64(nw)/2, float64(nh)/2
	// Inverse map: for each output pixel, rotate by -deg back into the source.
	ci, si := math.Cos(-rad), math.Sin(-rad)
	for ny := 0; ny < nh; ny++ {
		dy := float64(ny) - ncy
		for nx := 0; nx < nw; nx++ {
			dx := float64(nx) - ncx
			sx := ci*dx - si*dy + cx
			sy := si*dx + ci*dy + cy
			r, g, b, ok := bilinear(im, sx, sy)
			if !ok {
				continue
			}
			di := (ny*nw + nx) * 3
			out.Pix[di] = r
			out.Pix[di+1] = g
			out.Pix[di+2] = b
		}
	}
	return out
}

// bilinear samples the image at (x,y); ok=false when fully outside.
func bilinear(im *image.Image, x, y float64) (float32, float32, float32, bool) {
	if x < -0.5 || y < -0.5 || x > float64(im.W)-0.5 || y > float64(im.H)-0.5 {
		return 0, 0, 0, false
	}
	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	fx := x - float64(x0)
	fy := y - float64(y0)
	x1, y1 := x0+1, y0+1
	x0 = clampi(x0, 0, im.W-1)
	x1 = clampi(x1, 0, im.W-1)
	y0 = clampi(y0, 0, im.H-1)
	y1 = clampi(y1, 0, im.H-1)
	var out [3]float32
	for c := 0; c < 3; c++ {
		p00 := float64(im.Pix[(y0*im.W+x0)*3+c])
		p01 := float64(im.Pix[(y0*im.W+x1)*3+c])
		p10 := float64(im.Pix[(y1*im.W+x0)*3+c])
		p11 := float64(im.Pix[(y1*im.W+x1)*3+c])
		top := p00 + fx*(p01-p00)
		bot := p10 + fx*(p11-p10)
		out[c] = float32(top + fy*(bot-top))
	}
	return out[0], out[1], out[2], true
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
