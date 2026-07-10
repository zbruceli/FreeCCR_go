// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

package convert

import (
	"math"
	"sort"

	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// RefParams are the per-channel percentile anchors and OD alignment factors
// derived from the reference frame, replayable at any resolution.
type RefParams struct {
	PLo, PHi  [3]float64
	ODFactors [3]float64
}

// ComputeReferenceNormParams derives the reference-frame normalization params
// from a crop of the reference image. crop is a *image.Image containing only the
// reference rectangle (already extracted/rotated by the caller). Mirrors
// compute_reference_norm_params (ccr_processor.py:1769); the fine-rotation and
// rect-mapping steps are the caller's responsibility.
func ComputeReferenceNormParams(crop *image.Image) RefParams {
	n := crop.Pixels()
	var p RefParams
	// Per-channel percentiles over the crop.
	chans := make([][]float64, 3)
	for c := 0; c < 3; c++ {
		chans[c] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for c := 0; c < 3; c++ {
			chans[c][i] = float64(crop.Pix[i*3+c])
		}
	}
	var meanOD [3]float64
	for c := 0; c < 3; c++ {
		p.PLo[c] = percentileLinear(chans[c], 1)
		p.PHi[c] = percentileLinear(chans[c], 99)
		// Normalized crop → optical density mean, matching the Python exactly.
		rng := p.PHi[c] - p.PLo[c]
		var sum float64
		for i := 0; i < n; i++ {
			nv := (chans[c][i]-p.PLo[c])/rng*(65535-8192) + 8192
			if nv < 0 {
				nv = 0
			} else if nv > 65535 {
				nv = 65535
			}
			od := -math.Log10((nv + 1e-6) / 65535.0)
			sum += od
		}
		meanOD[c] = sum / float64(n)
	}
	target := (meanOD[0] + meanOD[1] + meanOD[2]) / 3.0
	for c := 0; c < 3; c++ {
		p.ODFactors[c] = target / (meanOD[c] + 1e-12)
	}
	return p
}

// ApplyReferenceNormalization applies the reference normalization + inversion +
// post-invert look at any resolution using precomputed params. Mirrors
// apply_reference_normalization (ccr_processor.py:1801): the OD alignment
// od=-log10(v); od*=f; 10^-od collapses to v^f, computed directly.
func ApplyReferenceNormalization(im *image.Image, p RefParams) *image.Image {
	var a, b, f [3]float64
	for c := 0; c < 3; c++ {
		s := (65535.0 - 8192.0) / (p.PHi[c] - p.PLo[c])
		a[c] = s / 65535.0
		b[c] = (8192.0 - p.PLo[c]*s) / 65535.0
		f[c] = p.ODFactors[c]
	}
	const eps = 1e-6 / 65535.0

	// Fused single pass: per-channel normalize + OD power + invert, then the
	// post-invert look inline (it is a per-pixel function of the inverted
	// values). Bit-identical to the two-pass form, half the memory traffic.
	out := newLike(im)
	px := im.Pix
	op := out.Pix
	par.Rows(im.H, func(lo, hi int) {
		for i := lo * im.W * 3; i < hi*im.W*3; i += 3 {
			var inv0, inv1, inv2 float32
			for c := 0; c < 3; c++ {
				nv := clip01(float64(px[i+c])*a[c] + b[c])
				nv += eps
				nv = math.Pow(nv, f[c])
				nv *= 65535.0
				q := quantU16(float32(nv)) // clip+truncate to uint16
				switch c {
				case 0:
					inv0 = 65535.0 - q
				case 1:
					inv1 = 65535.0 - q
				case 2:
					inv2 = 65535.0 - q
				}
			}
			op[i], op[i+1], op[i+2] = postInvertPixel(inv0, inv1, inv2)
		}
	})
	return out
}

// percentileLinear reproduces numpy.percentile(a, q) with the default linear
// interpolation method. q is in [0,100]. It sorts a copy of the data.
func percentileLinear(data []float64, q float64) float64 {
	n := len(data)
	if n == 0 {
		return 0
	}
	s := make([]float64, n)
	copy(s, data)
	sort.Float64s(s)
	if n == 1 {
		return s[0]
	}
	// Virtual index into the sorted array: rank = q/100 * (n-1).
	rank := q / 100.0 * float64(n-1)
	lo := int(math.Floor(rank))
	hi := lo + 1
	frac := rank - float64(lo)
	if hi >= n {
		return s[n-1]
	}
	return s[lo] + frac*(s[hi]-s[lo])
}
