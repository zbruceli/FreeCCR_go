// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Package export writes processed images to disk: 16-bit RGB TIFF (scientific
// output) and 8-bit JPEG (web-ready), mirroring FreeCCR's export formats.
package export

import (
	"bufio"
	"encoding/binary"
	stdimage "image"
	"image/jpeg"
	"math"
	"os"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// to8 converts a 16-bit channel value to 8-bit with rounding + saturation,
// matching cv2.convertScaleAbs(img16, alpha=255/65535) used by FreeCCR's
// to_8bit.
func to8(v float32) uint8 {
	u := math.Round(float64(v) * (255.0 / 65535.0))
	if u < 0 {
		u = 0
	} else if u > 255 {
		u = 255
	}
	return uint8(u)
}

// WriteJPEG writes an 8-bit RGB JPEG at the given quality (1-100).
func WriteJPEG(path string, im *image.Image, quality int) error {
	rgba := stdimage.NewRGBA(stdimage.Rect(0, 0, im.W, im.H))
	par.Rows(im.H, func(lo, hi int) {
		for y := lo; y < hi; y++ {
			si := y * im.W * 3
			di := rgba.PixOffset(0, y)
			for x := 0; x < im.W; x++ {
				rgba.Pix[di] = to8(im.Pix[si])
				rgba.Pix[di+1] = to8(im.Pix[si+1])
				rgba.Pix[di+2] = to8(im.Pix[si+2])
				rgba.Pix[di+3] = 255
				si += 3
				di += 4
			}
		}
	})
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	if err := jpeg.Encode(bw, rgba, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}
	return bw.Flush()
}

// WriteTIFF16 writes an uncompressed 16-bit RGB TIFF (big-endian), the
// scientific-output format. Channel values are clamped to [0,65535] and floored.
func WriteTIFF16(path string, im *image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	if err := writeTIFF16(bw, im); err != nil {
		return err
	}
	return bw.Flush()
}

// writeTIFF16 emits a minimal baseline TIFF: 8-byte header, pixel strip, then a
// single IFD. big-endian ("MM"). RGB, 3×16 bits/sample, no compression.
func writeTIFF16(w *bufio.Writer, im *image.Image) error {
	const bo = "MM"
	npix := im.W * im.H
	dataLen := npix * 3 * 2

	// Layout: header(8) | pixel data(dataLen) | IFD.
	ifdOffset := uint32(8 + dataLen)

	// --- header ---
	if _, err := w.WriteString(bo); err != nil {
		return err
	}
	var b4 [4]byte
	binary.BigEndian.PutUint16(b4[:2], 42)
	if _, err := w.Write(b4[:2]); err != nil {
		return err
	}
	binary.BigEndian.PutUint32(b4[:], ifdOffset)
	if _, err := w.Write(b4[:]); err != nil {
		return err
	}

	// --- pixel data (RGB, 16-bit big-endian) ---
	buf := make([]byte, im.W*3*2)
	for y := 0; y < im.H; y++ {
		si := y * im.W * 3
		bi := 0
		for x := 0; x < im.W*3; x++ {
			v := im.Pix[si+x]
			if v <= 0 {
				v = 0
			} else if v >= 65535 {
				v = 65535
			}
			u := uint16(v)
			buf[bi] = byte(u >> 8)
			buf[bi+1] = byte(u)
			bi += 2
		}
		if _, err := w.Write(buf); err != nil {
			return err
		}
	}

	// --- IFD ---
	// BitsPerSample needs an out-of-line array of 3 shorts; place it right
	// after the IFD body. StripByteCounts also needs an out-of-line long (one
	// strip). We use a single strip covering the whole image.
	type entry struct {
		tag, typ uint16
		count    uint32
		value    uint32 // inline value or offset
	}
	entries := []entry{
		{256, 3, 1, uint32(im.W)},    // ImageWidth (SHORT)
		{257, 3, 1, uint32(im.H)},    // ImageLength
		{258, 3, 3, 0},               // BitsPerSample → offset (filled below)
		{259, 3, 1, 1},               // Compression = none
		{262, 3, 1, 2},               // PhotometricInterpretation = RGB
		{273, 4, 1, 8},               // StripOffsets → pixel data at offset 8
		{277, 3, 1, 3},               // SamplesPerPixel
		{278, 3, 1, uint32(im.H)},    // RowsPerStrip = full image
		{279, 4, 1, uint32(dataLen)}, // StripByteCounts
	}
	nEntries := uint16(len(entries))
	// IFD size: 2 (count) + 12*n + 4 (next IFD). Then out-of-line BitsPerSample.
	ifdBody := 2 + 12*int(nEntries) + 4
	bpsOffset := ifdOffset + uint32(ifdBody)

	// count
	var b2 [2]byte
	binary.BigEndian.PutUint16(b2[:], nEntries)
	if _, err := w.Write(b2[:]); err != nil {
		return err
	}
	for _, e := range entries {
		if e.tag == 258 {
			e.value = bpsOffset
		}
		var eb [12]byte
		binary.BigEndian.PutUint16(eb[0:2], e.tag)
		binary.BigEndian.PutUint16(eb[2:4], e.typ)
		binary.BigEndian.PutUint32(eb[4:8], e.count)
		// SHORT values with count 1 are left-justified in the 4-byte value field.
		if e.typ == 3 && e.count == 1 {
			binary.BigEndian.PutUint16(eb[8:10], uint16(e.value))
		} else {
			binary.BigEndian.PutUint32(eb[8:12], e.value)
		}
		if _, err := w.Write(eb[:]); err != nil {
			return err
		}
	}
	// next IFD = 0
	binary.BigEndian.PutUint32(b4[:], 0)
	if _, err := w.Write(b4[:]); err != nil {
		return err
	}
	// out-of-line BitsPerSample = [16,16,16]
	var bps [6]byte
	binary.BigEndian.PutUint16(bps[0:2], 16)
	binary.BigEndian.PutUint16(bps[2:4], 16)
	binary.BigEndian.PutUint16(bps[4:6], 16)
	if _, err := w.Write(bps[:]); err != nil {
		return err
	}
	return nil
}
