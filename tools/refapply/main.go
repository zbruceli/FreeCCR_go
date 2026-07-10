// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Applies reference normalization with explicit params (A/B diagnostic).
package main

import (
	"flag"
	"strconv"
	"strings"

	"github.com/zhengli/freeccr-go/internal/convert"
	"github.com/zhengli/freeccr-go/internal/decode"
	"github.com/zhengli/freeccr-go/internal/export"
)

func p3(s string) [3]float64 {
	var o [3]float64
	for i, v := range strings.Split(s, ",") {
		f, _ := strconv.ParseFloat(v, 64)
		o[i] = f
	}
	return o
}

func main() {
	in := flag.String("i", "", "input")
	out := flag.String("o", "", "output")
	plo := flag.String("plo", "", "p_lo")
	phi := flag.String("phi", "", "p_hi")
	odf := flag.String("odf", "", "od_factors")
	flag.Parse()
	im, err := decode.DecodeStandard(*in)
	if err != nil {
		panic(err)
	}
	pr := convert.RefParams{PLo: p3(*plo), PHi: p3(*phi), ODFactors: p3(*odf)}
	res := convert.ApplyReferenceNormalization(im, pr)
	if err := export.WriteTIFF16(*out, res); err != nil {
		panic(err)
	}
}
