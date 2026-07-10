// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Package decode reads image files into the pipeline's RGB float32 buffer.
//
// The working buffer is RGB (channel 0=R), matching FreeCCR's non-RAW decode
// (ccr_image.read_image: cv2 BGR → COLOR_BGR2RGB). Bit-depth is normalized to
// the 16-bit full-range contract: 16-bit as-is, 8-bit ×257, float→round — which
// Go's color.Color.RGBA() reproduces exactly (its 8-bit model multiplies by
// 0x101 == 257, 16-bit passes through).
package decode

import (
	"fmt"
	stdimage "image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"

	_ "golang.org/x/image/tiff"
)

// StandardExts are the non-RAW extensions handled here.
var StandardExts = map[string]bool{
	".tif": true, ".tiff": true, ".fff": true,
	".jpg": true, ".jpeg": true, ".png": true,
}

// IsStandard reports whether path has a supported standard-format extension.
func IsStandard(path string) bool {
	return StandardExts[strings.ToLower(filepath.Ext(path))]
}

// DecodeStandard reads a TIFF/JPEG/PNG file into an RGB float32 Image with
// values in [0,65535].
func DecodeStandard(path string) (*image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, _, err := stdimage.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return FromGoImage(src), nil
}

// FromGoImage converts a decoded Go image into the RGB float32 buffer.
func FromGoImage(src stdimage.Image) *image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	im := image.New(w, h)
	// Fast paths avoid the per-pixel interface dispatch of At().RGBA() for the
	// common concrete types; the generic path is correct for everything else.
	switch s := src.(type) {
	case *stdimage.NRGBA64:
		fillNRGBA64(im, s, b)
	case *stdimage.RGBA64:
		fillRGBA64(im, s, b)
	case *stdimage.NRGBA:
		fillNRGBA(im, s, b)
	case *stdimage.RGBA:
		fillRGBA(im, s, b)
	default:
		fillGeneric(im, src, b)
	}
	return im
}

func fillGeneric(im *image.Image, src stdimage.Image, b stdimage.Rectangle) {
	par.Rows(im.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			o := y * im.W * 3
			for x := 0; x < im.W; x++ {
				r, g, bl, _ := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
				im.Pix[o] = float32(r)
				im.Pix[o+1] = float32(g)
				im.Pix[o+2] = float32(bl)
				o += 3
			}
		}
	})
}

func fillNRGBA64(im *image.Image, s *stdimage.NRGBA64, b stdimage.Rectangle) {
	par.Rows(im.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			o := y * im.W * 3
			row := s.PixOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < im.W; x++ {
				p := s.Pix[row : row+6 : row+6]
				im.Pix[o] = float32(uint16(p[0])<<8 | uint16(p[1]))
				im.Pix[o+1] = float32(uint16(p[2])<<8 | uint16(p[3]))
				im.Pix[o+2] = float32(uint16(p[4])<<8 | uint16(p[5]))
				o += 3
				row += 8
			}
		}
	})
}

func fillRGBA64(im *image.Image, s *stdimage.RGBA64, b stdimage.Rectangle) {
	par.Rows(im.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			o := y * im.W * 3
			row := s.PixOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < im.W; x++ {
				p := s.Pix[row : row+8 : row+8]
				im.Pix[o] = float32(uint16(p[0])<<8 | uint16(p[1]))
				im.Pix[o+1] = float32(uint16(p[2])<<8 | uint16(p[3]))
				im.Pix[o+2] = float32(uint16(p[4])<<8 | uint16(p[5]))
				o += 3
				row += 8
			}
		}
	})
}

func fillNRGBA(im *image.Image, s *stdimage.NRGBA, b stdimage.Rectangle) {
	par.Rows(im.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			o := y * im.W * 3
			row := s.PixOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < im.W; x++ {
				p := s.Pix[row : row+4 : row+4]
				im.Pix[o] = float32(uint16(p[0]) * 257)
				im.Pix[o+1] = float32(uint16(p[1]) * 257)
				im.Pix[o+2] = float32(uint16(p[2]) * 257)
				o += 3
				row += 4
			}
		}
	})
}

func fillRGBA(im *image.Image, s *stdimage.RGBA, b stdimage.Rectangle) {
	// RGBA is alpha-premultiplied; for opaque scans alpha=255 so R/G/B are
	// straight values. ×257 matches the 8-bit→16-bit contract.
	par.Rows(im.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			o := y * im.W * 3
			row := s.PixOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < im.W; x++ {
				p := s.Pix[row : row+4 : row+4]
				im.Pix[o] = float32(uint16(p[0]) * 257)
				im.Pix[o+1] = float32(uint16(p[1]) * 257)
				im.Pix[o+2] = float32(uint16(p[2]) * 257)
				o += 3
				row += 4
			}
		}
	})
}
