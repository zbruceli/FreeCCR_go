// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package export

import (
	"bufio"
	"encoding/binary"
	"os"

	"github.com/zhengli/freeccr-go/internal/image"
)

// WriteDNG writes the processed 16-bit RGB image as a Linear DNG — a demosaiced
// (PhotometricInterpretation = LinearRaw 34892) single-IFD DNG with the
// mandatory DNG tags, so the converted positive imports into raw editors
// (Lightroom / Camera Raw / RawTherapee). The stored primaries are sRGB
// (ColorMatrix1 = XYZ→linear sRGB, D65); AsShotNeutral is (1,1,1) since the data
// is already balanced. Little-endian ("II"), uncompressed.
func WriteDNG(path string, im *image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	if err := writeDNG(bw, im); err != nil {
		return err
	}
	return bw.Flush()
}

func writeDNG(w *bufio.Writer, im *image.Image) error {
	le := binary.LittleEndian
	W, H := uint32(im.W), uint32(im.H)
	dataLen := uint32(im.W * im.H * 3 * 2)

	// --- out-of-line value blobs (word-aligned) ---
	bps := []byte{16, 0, 16, 0, 16, 0}    // BitsPerSample [16,16,16]
	sampleFmt := []byte{1, 0, 1, 0, 1, 0} // SampleFormat  [uint,uint,uint]
	model := []byte("FreeCCR-go\x00\x00") // UniqueCameraModel (padded even)
	// ColorMatrix1 (XYZ D65 → linear sRGB), SRATIONAL/10000.
	mat := []int32{32406, -15372, -4986, -9689, 18758, 415, 557, -2040, 10570}
	cm := make([]byte, 72)
	for i, v := range mat {
		le.PutUint32(cm[i*8:], uint32(v))
		le.PutUint32(cm[i*8+4:], 10000)
	}
	asn := make([]byte, 24) // AsShotNeutral [1,1,1]
	for i := 0; i < 3; i++ {
		le.PutUint32(asn[i*8:], 1)
		le.PutUint32(asn[i*8+4:], 1)
	}

	type entry struct {
		tag, typ uint16
		count    uint32
		inline   [4]byte
		out      []byte // nil ⇒ inline
	}
	short1 := func(v uint16) [4]byte { var a [4]byte; le.PutUint16(a[:], v); return a }
	long1 := func(v uint32) [4]byte { var a [4]byte; le.PutUint32(a[:], v); return a }
	b4 := func(bs ...byte) [4]byte { var a [4]byte; copy(a[:], bs); return a }

	// Tags MUST be in ascending order. Types: BYTE=1 ASCII=2 SHORT=3 LONG=4
	// RATIONAL=5 SRATIONAL=10.
	entries := []entry{
		{254, 4, 1, long1(0), nil},                       // NewSubFileType = main image
		{256, 4, 1, long1(W), nil},                       // ImageWidth
		{257, 4, 1, long1(H), nil},                       // ImageLength
		{258, 3, 3, [4]byte{}, bps},                      // BitsPerSample
		{259, 3, 1, short1(1), nil},                      // Compression = none
		{262, 3, 1, short1(34892), nil},                  // PhotometricInterpretation = LinearRaw
		{273, 4, 1, long1(8), nil},                       // StripOffsets (pixel data at 8)
		{277, 3, 1, short1(3), nil},                      // SamplesPerPixel
		{278, 4, 1, long1(H), nil},                       // RowsPerStrip = whole image
		{279, 4, 1, long1(dataLen), nil},                 // StripByteCounts
		{284, 3, 1, short1(1), nil},                      // PlanarConfiguration = chunky
		{339, 3, 3, [4]byte{}, sampleFmt},                // SampleFormat
		{50706, 1, 4, b4(1, 4, 0, 0), nil},               // DNGVersion 1.4.0.0
		{50707, 1, 4, b4(1, 1, 0, 0), nil},               // DNGBackwardVersion 1.1.0.0
		{50708, 2, uint32(len(model)), [4]byte{}, model}, // UniqueCameraModel
		{50717, 4, 1, long1(65535), nil},                 // WhiteLevel
		{50721, 10, 9, [4]byte{}, cm},                    // ColorMatrix1
		{50728, 5, 3, [4]byte{}, asn},                    // AsShotNeutral
		{50778, 3, 1, short1(21), nil},                   // CalibrationIlluminant1 = D65
	}

	n := len(entries)
	ifdOffset := 8 + dataLen
	ifdSize := uint32(2 + 12*n + 4)
	outBase := ifdOffset + ifdSize
	// Assign out-of-line offsets sequentially.
	off := outBase
	for i := range entries {
		if entries[i].out != nil {
			entries[i].inline = long1(off)
			off += uint32(len(entries[i].out))
		}
	}

	// --- header ---
	if _, err := w.WriteString("II"); err != nil {
		return err
	}
	var tmp [4]byte
	le.PutUint16(tmp[:2], 42)
	if _, err := w.Write(tmp[:2]); err != nil {
		return err
	}
	le.PutUint32(tmp[:], ifdOffset)
	if _, err := w.Write(tmp[:]); err != nil {
		return err
	}

	// --- pixel data (16-bit LE RGB, interleaved) ---
	rowBuf := make([]byte, im.W*3*2)
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
			rowBuf[bi] = byte(u)
			rowBuf[bi+1] = byte(u >> 8)
			bi += 2
		}
		if _, err := w.Write(rowBuf); err != nil {
			return err
		}
	}

	// --- IFD ---
	le.PutUint16(tmp[:2], uint16(n))
	if _, err := w.Write(tmp[:2]); err != nil {
		return err
	}
	for _, e := range entries {
		var eb [12]byte
		le.PutUint16(eb[0:2], e.tag)
		le.PutUint16(eb[2:4], e.typ)
		le.PutUint32(eb[4:8], e.count)
		copy(eb[8:12], e.inline[:])
		if _, err := w.Write(eb[:]); err != nil {
			return err
		}
	}
	le.PutUint32(tmp[:], 0) // next IFD = none
	if _, err := w.Write(tmp[:]); err != nil {
		return err
	}

	// --- out-of-line value blobs, in entry order ---
	for _, e := range entries {
		if e.out != nil {
			if _, err := w.Write(e.out); err != nil {
				return err
			}
		}
	}
	return nil
}
