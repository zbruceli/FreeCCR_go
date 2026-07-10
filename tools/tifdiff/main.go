// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Command tifdiff compares two images channel-by-channel (max/mean abs diff).
package main

import (
	"fmt"
	"os"

	"github.com/zhengli/freeccr-go/internal/decode"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: tifdiff a.tif b.tif")
		os.Exit(2)
	}
	a, err := decode.DecodeStandard(os.Args[1])
	if err != nil {
		panic(err)
	}
	b, err := decode.DecodeStandard(os.Args[2])
	if err != nil {
		panic(err)
	}
	if a.W != b.W || a.H != b.H {
		fmt.Printf("DIM MISMATCH %dx%d vs %dx%d\n", a.W, a.H, b.W, b.H)
		os.Exit(1)
	}
	var maxDiff, nBad int
	var sum float64
	hist := map[int]int{}
	for i := range a.Pix {
		d := int(a.Pix[i]) - int(b.Pix[i])
		if d < 0 {
			d = -d
		}
		sum += float64(d)
		if d > maxDiff {
			maxDiff = d
		}
		if d > 0 {
			nBad++
		}
		bucket := d
		if bucket > 4 {
			bucket = 5 // "5+"
		}
		hist[bucket]++
	}
	n := len(a.Pix)
	fmt.Printf("channels=%d  maxAbsDiff=%d  meanAbsDiff=%.4f  differing=%.3f%%\n",
		n, maxDiff, sum/float64(n), 100*float64(nBad)/float64(n))
	for d := 0; d <= 5; d++ {
		label := fmt.Sprintf("%d", d)
		if d == 5 {
			label = "5+"
		}
		fmt.Printf("  diff=%s: %.3f%%\n", label, 100*float64(hist[d])/float64(n))
	}
}
