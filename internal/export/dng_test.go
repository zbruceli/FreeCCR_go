// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package export

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhengli/freeccr-go/internal/image"
)

// TestDNGStructure verifies the written DNG is a well-formed little-endian TIFF
// with the mandatory linear-DNG tags (PhotometricInterpretation=LinearRaw,
// DNGVersion, dims), and that the pixel strip round-trips exactly.
func TestDNGStructure(t *testing.T) {
	w, h := 9, 7
	im := image.New(w, h)
	for i := range im.Pix {
		im.Pix[i] = float32((i * 613) % 65536)
	}
	path := filepath.Join(t.TempDir(), "t.dng")
	if err := WriteDNG(path, im); err != nil {
		t.Fatalf("write: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	if string(b[:2]) != "II" || le.Uint16(b[2:4]) != 42 {
		t.Fatalf("bad TIFF header: %q", b[:4])
	}
	ifd := le.Uint32(b[4:8])
	n := int(le.Uint16(b[ifd : ifd+2]))
	tags := map[uint16][]byte{}
	for i := 0; i < n; i++ {
		e := b[ifd+2+uint32(i)*12:]
		tags[le.Uint16(e[0:2])] = e[8:12]
	}
	must := []uint16{256, 257, 258, 262, 273, 277, 50706, 50721, 50778}
	for _, tg := range must {
		if _, ok := tags[tg]; !ok {
			t.Errorf("missing required DNG tag %d", tg)
		}
	}
	if got := le.Uint16(tags[262]); got != 34892 {
		t.Errorf("PhotometricInterpretation = %d, want 34892 (LinearRaw)", got)
	}
	if got := le.Uint32(tags[256]); got != uint32(w) {
		t.Errorf("ImageWidth = %d, want %d", got, w)
	}
	if got := le.Uint32(tags[257]); got != uint32(h) {
		t.Errorf("ImageLength = %d, want %d", got, h)
	}
	if v := tags[50706]; v[0] != 1 || v[1] != 4 {
		t.Errorf("DNGVersion = %v, want 1.4.x", v[:2])
	}
	// Pixel strip starts at offset 8; verify a couple of values round-trip LE.
	for _, idx := range []int{0, 1, len(im.Pix) - 1} {
		got := le.Uint16(b[8+idx*2:])
		want := uint16(im.Pix[idx])
		if got != want {
			t.Errorf("pixel[%d] = %d, want %d", idx, got, want)
		}
	}
}
