// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package decode

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zhengli/freeccr-go/internal/image"
)

// RawExts are the RAW extensions routed to the libraw decoder (mirrors
// ccr_image.read_image's RAW branch).
var RawExts = map[string]bool{
	".cr3": true, ".cr2": true, ".nef": true, ".arw": true, ".dng": true,
	".rw2": true, ".orf": true, ".raf": true, ".srw": true, ".pef": true,
	".3fr": true,
}

// IsRAW reports whether path has a RAW extension.
func IsRAW(path string) bool {
	return RawExts[strings.ToLower(filepath.Ext(path))]
}

// Decode reads any supported image file into an RGB float32 Image. RAW files go
// through libraw (requires a build with the `libraw` tag); standard formats use
// the pure-Go path. preview=true decodes RAW at half size.
func Decode(path string, preview bool) (*image.Image, error) {
	if IsRAW(path) {
		if !RawAvailable {
			return nil, fmt.Errorf("RAW file %s requires a build with libraw (build with -tags libraw)", filepath.Base(path))
		}
		return decodeRAW(path, preview)
	}
	if IsStandard(path) {
		return DecodeStandard(path)
	}
	return nil, fmt.Errorf("unsupported format: %s", filepath.Base(path))
}
