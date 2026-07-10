// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package decode

import (
	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// ResizeToMaxSide downscales im so its longest side is at most maxSide,
// preserving aspect ratio. If the image is already within bounds it is returned
// unchanged (not copied). Used to build ~1080px previews for interactive work;
// the B/W anchors sampled from a preview stay valid at full resolution because
// no-auto-bright decoding keeps scan values absolute.
func ResizeToMaxSide(im *image.Image, maxSide int) *image.Image {
	long := im.W
	if im.H > long {
		long = im.H
	}
	if long <= maxSide || maxSide <= 0 {
		return im
	}
	scale := float64(maxSide) / float64(long)
	dw := int(float64(im.W)*scale + 0.5)
	dh := int(float64(im.H)*scale + 0.5)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	return Resize(im, dw, dh)
}

// Resize performs an area-average downscale (box filter) to dstW×dstH. Each
// destination pixel averages the source pixels in its footprint — a good
// approximation of OpenCV's INTER_AREA for preview/thumbnail use. Intended for
// downscaling; for upscaling it falls back to nearest-neighbor.
func Resize(src *image.Image, dstW, dstH int) *image.Image {
	dst := image.New(dstW, dstH)
	sw, sh := src.W, src.H
	sx := float64(sw) / float64(dstW)
	sy := float64(sh) / float64(dstH)

	par.Rows(dstH, func(lo, hi int) {
		for dy := lo; dy < hi; dy++ {
			y0 := int(float64(dy) * sy)
			y1 := int(float64(dy+1) * sy)
			if y1 <= y0 {
				y1 = y0 + 1
			}
			if y1 > sh {
				y1 = sh
			}
			do := (dy * dstW) * 3
			for dx := 0; dx < dstW; dx++ {
				x0 := int(float64(dx) * sx)
				x1 := int(float64(dx+1) * sx)
				if x1 <= x0 {
					x1 = x0 + 1
				}
				if x1 > sw {
					x1 = sw
				}
				var r, g, b float64
				n := 0
				for yy := y0; yy < y1; yy++ {
					so := (yy*sw + x0) * 3
					for xx := x0; xx < x1; xx++ {
						r += float64(src.Pix[so])
						g += float64(src.Pix[so+1])
						b += float64(src.Pix[so+2])
						so += 3
						n++
					}
				}
				inv := 1.0 / float64(n)
				dst.Pix[do] = float32(r * inv)
				dst.Pix[do+1] = float32(g * inv)
				dst.Pix[do+2] = float32(b * inv)
				do += 3
			}
		}
	})
	return dst
}
