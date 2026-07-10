// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

//go:build !libraw

package decode

import (
	"errors"

	"github.com/zhengli/freeccr-go/internal/image"
)

// RawAvailable is false in builds without the `libraw` tag.
const RawAvailable = false

func decodeRAW(path string, preview bool) (*image.Image, error) {
	return nil, errors.New("built without libraw support")
}
