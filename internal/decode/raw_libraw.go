// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

//go:build libraw

package decode

// RAW decoding via libraw (cgo). Mirrors FreeCCR's default negative decode
// (ccr_image._raw_color_postprocess_kwargs with no_icc_default=True): AHD
// demosaic, 16-bit linear, no auto-bright, no camera WB, output in Adobe RGB,
// libraw auto-scaled to full range (so no manual white-level scaling). The
// processed bitmap is interleaved RGB uint16, matching the pipeline's channel
// order.
//
// Build:  go build -tags libraw ./...   (requires `brew install libraw`)

/*
#cgo pkg-config: libraw
#include <libraw/libraw.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// RawAvailable is true in builds with the `libraw` tag.
const RawAvailable = true

func decodeRAW(path string, preview bool) (*image.Image, error) {
	lr := C.libraw_init(0)
	if lr == nil {
		return nil, fmt.Errorf("libraw_init failed")
	}
	defer C.libraw_close(lr)

	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if r := C.libraw_open_file(lr, cpath); r != 0 {
		return nil, fmt.Errorf("libraw open %s: %s", path, C.GoString(C.libraw_strerror(r)))
	}

	// Postprocess params — the unprofiled negative decode.
	p := &lr.params
	p.output_bps = 16
	p.no_auto_bright = 1
	p.gamm[0] = 1.0 // linear gamma (dcraw -g 1 1)
	p.gamm[1] = 1.0
	p.user_flip = 0
	p.user_qual = 3 // AHD demosaic
	p.use_camera_wb = 0
	p.use_auto_wb = 0
	p.output_color = 2  // Adobe RGB
	p.no_auto_scale = 0 // libraw auto-scales to full 16-bit range
	p.adjust_maximum_thr = 0.0
	p.four_color_rgb = 0
	if preview {
		p.half_size = 1
	} else {
		p.half_size = 0
	}

	if r := C.libraw_unpack(lr); r != 0 {
		return nil, fmt.Errorf("libraw unpack %s: %s", path, C.GoString(C.libraw_strerror(r)))
	}
	if r := C.libraw_dcraw_process(lr); r != 0 {
		return nil, fmt.Errorf("libraw process %s: %s", path, C.GoString(C.libraw_strerror(r)))
	}

	var errc C.int
	pimg := C.libraw_dcraw_make_mem_image(lr, &errc)
	if pimg == nil || errc != 0 {
		return nil, fmt.Errorf("libraw make_mem_image %s: code %d", path, int(errc))
	}
	defer C.libraw_dcraw_clear_mem(pimg)

	if pimg._type != C.LIBRAW_IMAGE_BITMAP {
		return nil, fmt.Errorf("libraw %s: not a bitmap image", path)
	}
	w, h := int(pimg.width), int(pimg.height)
	colors, bits := int(pimg.colors), int(pimg.bits)
	if bits != 16 {
		return nil, fmt.Errorf("libraw %s: expected 16-bit, got %d-bit", path, bits)
	}
	if colors != 3 && colors != 1 {
		return nil, fmt.Errorf("libraw %s: unexpected color count %d", path, colors)
	}

	n := w * h * colors
	data := unsafe.Slice((*uint16)(unsafe.Pointer(&pimg.data[0])), n)

	im := image.New(w, h)
	if colors == 3 {
		par.Rows(h, func(lo, hi int) {
			for y := lo; y < hi; y++ {
				o := y * w * 3
				for x := 0; x < w*3; x++ {
					im.Pix[o+x] = float32(data[o+x])
				}
			}
		})
	} else { // monochrome: replicate the single channel across RGB
		par.Rows(h, func(lo, hi int) {
			for y := lo; y < hi; y++ {
				si := y * w
				di := y * w * 3
				for x := 0; x < w; x++ {
					v := float32(data[si+x])
					im.Pix[di] = v
					im.Pix[di+1] = v
					im.Pix[di+2] = v
					di += 3
				}
			}
		})
	}
	return im, nil
}
