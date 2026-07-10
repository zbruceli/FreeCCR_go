// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package export

import (
	"fmt"
	"strings"

	"github.com/zhengli/freeccr-go/internal/image"
)

// Ext returns the filename extension for an output format ("tif", "jpg", "dng").
func Ext(format string) string {
	switch strings.ToLower(format) {
	case "jpg", "jpeg":
		return ".jpg"
	case "dng":
		return ".dng"
	default:
		return ".tif"
	}
}

// Write saves im in the given format ("tif" 16-bit, "jpg" 8-bit, "dng" linear).
// quality is used only for JPEG.
func Write(path string, im *image.Image, format string, quality int) error {
	switch strings.ToLower(format) {
	case "jpg", "jpeg":
		return WriteJPEG(path, im, quality)
	case "dng":
		return WriteDNG(path, im)
	case "tif", "tiff", "":
		return WriteTIFF16(path, im)
	default:
		return fmt.Errorf("unknown export format %q", format)
	}
}
