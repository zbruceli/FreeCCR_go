// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Command genneg writes synthetic color-negative TIFF frames for pipeline and
// batch testing. With -n>1 it writes a "roll" of frames into -dir.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/zhengli/freeccr-go/internal/export"
	"github.com/zhengli/freeccr-go/internal/image"
)

func main() {
	n := flag.Int("n", 1, "number of frames")
	dir := flag.String("dir", ".", "output directory")
	w := flag.Int("w", 1200, "width")
	h := flag.Int("h", 800, "height")
	flag.Parse()

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		panic(err)
	}
	for f := 0; f < *n; f++ {
		im := frame(*w, *h, f)
		name := "neg.tif"
		if *n > 1 {
			name = fmt.Sprintf("neg_%03d.tif", f)
		}
		path := filepath.Join(*dir, name)
		if err := export.WriteTIFF16(path, im); err != nil {
			panic(err)
		}
		image.PutBuf(im.Pix)
	}
	fmt.Printf("wrote %d frame(s) %dx%d → %s\n", *n, *w, *h, *dir)
}

func frame(w, h, seed int) *image.Image {
	im := image.New(w, h)
	baseR, baseG, baseB := float32(61000), float32(58000), float32(52000)
	ph := float64(seed) * 0.7
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 3
			l := float32(x) / float32(w)
			cr := 0.5 + 0.5*float32(math.Sin(float64(x)/90+ph))
			cg := 0.5 + 0.5*float32(math.Sin(float64(y)/70+ph))
			cb := 0.5 + 0.5*float32(math.Cos(float64(x+y)/110+ph))
			im.Pix[i] = baseR * (1 - 0.85*l*cr)
			im.Pix[i+1] = baseG * (1 - 0.85*l*cg)
			im.Pix[i+2] = baseB * (1 - 0.85*l*cb)
		}
	}
	return im
}
