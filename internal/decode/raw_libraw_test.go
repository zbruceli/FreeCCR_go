// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

//go:build libraw

package decode

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRAWMatchesDcrawEmu decodes a RAW via the libraw binding and asserts it is
// byte-identical to libraw's own dcraw_emu reference decode with the same
// params. Gated on FREECCR_TEST_RAW (path to a RAW file) and dcraw_emu in PATH;
// skips otherwise so the suite stays hermetic.
func TestRAWMatchesDcrawEmu(t *testing.T) {
	raw := os.Getenv("FREECCR_TEST_RAW")
	if raw == "" {
		t.Skip("set FREECCR_TEST_RAW=/path/to/file.raw to run")
	}
	emu, err := exec.LookPath("dcraw_emu")
	if err != nil {
		t.Skip("dcraw_emu not in PATH")
	}

	got, err := decodeRAW(raw, false)
	if err != nil {
		t.Fatalf("decodeRAW: %v", err)
	}

	// Reference: same params as raw_libraw.go — -6 (16-bit) -W (no auto-bright)
	// -g 1 1 (linear) -q 3 (AHD) -o 2 (Adobe) -T (TIFF).
	tmp := t.TempDir()
	work := filepath.Join(tmp, filepath.Base(raw))
	if err := copyFile(raw, work); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(emu, "-6", "-W", "-g", "1", "1", "-q", "3", "-o", "2", "-T", work)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dcraw_emu: %v\n%s", err, out)
	}
	ref, err := DecodeStandard(work + ".tiff")
	if err != nil {
		t.Fatalf("decode reference: %v", err)
	}

	if got.W != ref.W || got.H != ref.H {
		t.Fatalf("dims got %dx%d want %dx%d", got.W, got.H, ref.W, ref.H)
	}
	maxDiff := 0
	for i := range ref.Pix {
		d := int(got.Pix[i]) - int(ref.Pix[i])
		if d < 0 {
			d = -d
		}
		if d > maxDiff {
			maxDiff = d
		}
	}
	if maxDiff != 0 {
		t.Fatalf("RAW decode differs from dcraw_emu: maxAbsDiff=%d", maxDiff)
	}
	t.Logf("RAW decode bit-exact vs dcraw_emu (%dx%d)", got.W, got.H)
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}
