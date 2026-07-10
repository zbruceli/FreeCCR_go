// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Package pipeline orchestrates whole-roll batch processing: a shared Spec
// (conversion + adjustment settings applied to every frame) plus a two-stage
// decode → process worker pipeline with pooled-buffer reuse.
package pipeline

import (
	"runtime"
	"sync"

	"github.com/zhengli/freeccr-go/internal/adjust"
	"github.com/zhengli/freeccr-go/internal/convert"
	"github.com/zhengli/freeccr-go/internal/decode"
	"github.com/zhengli/freeccr-go/internal/export"
	"github.com/zhengli/freeccr-go/internal/geometry"
	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
)

// Mode selects the conversion path.
type Mode int

const (
	ModeBWPoint   Mode = iota // two-point (HasWhite) or default-slope (!HasWhite)
	ModeReference             // percentile normalization + post-invert look
)

// Spec is the shared per-roll processing recipe. The same Spec is applied to
// every frame, so its inputs (B/W anchors, reference params, adjustment
// sliders) are computed once and reused — the roll-wide workflow.
type Spec struct {
	Mode       Mode
	Black      [3]float64
	White      [3]float64
	HasWhite   bool
	Density    bool
	Slopes     *[3]float64
	Ref        *convert.RefParams // shared params for ModeReference (optional)
	RefRect    [4]float64         // per-frame reference rect (used when Ref==nil)
	HasRefRect bool
	WS         bool // working-space windowing
	Adjust     adjust.Params
	// Post-slider "look" stages, applied in the app's order:
	// sliders → bands → gamma → curves → cineon.
	Bands          *adjust.BandSettings
	Gamma          float64
	GammaLuminance bool
	Curves         *adjust.Curves
	Cineon         bool
	// Geometry (applied after the look chain): rotate → flips → straighten → crop.
	Rotation     int // 0/90/180/270 clockwise
	HFlip, VFlip bool
	FineRotation float64    // degrees
	Crop         [4]float64 // normalized x0,y0,x1,y1
	HasCrop      bool
	CropEditing  bool // skip the crop (show the uncropped frame while editing)
}

// ConvertBase runs only the negative→positive conversion and returns the
// converted base plus whether it is a working-space windowed base. The caller
// owns the returned buffer. Used by the auto tools (auto-WB / auto-exposure),
// which operate on the base before adjustments.
func (s *Spec) ConvertBase(im *image.Image) (*image.Image, bool) {
	ws := s.WS
	switch s.Mode {
	case ModeReference:
		params := s.Ref
		if params == nil {
			crop := im.CropNormRect(s.RefRect[0], s.RefRect[1], s.RefRect[2], s.RefRect[3])
			p := convert.ComputeReferenceNormParams(crop)
			image.PutBuf(crop.Pix)
			params = &p
		}
		return convert.ApplyReferenceNormalization(im, *params), false
	default:
		if s.HasWhite {
			return convert.TwoPointInvert(im, s.Black, s.White, s.Density, ws), ws
		}
		return convert.DefaultSlopeInvert(im, s.Black, s.Slopes, ws), ws
	}
}

// Process runs the full conversion + adjustment chain on one decoded image and
// returns the final quantized image. The input buffer is left untouched (the
// caller owns it); intermediate buffers are recycled to the pool.
func (s *Spec) Process(im *image.Image) *image.Image {
	conv, ws := s.ConvertBase(im)
	final := adjust.AdjustImage(conv, s.Adjust, ws)
	image.PutBuf(conv.Pix)
	return s.applyGeometry(s.applyLook(final))
}

// applyGeometry runs orientation + crop after the look chain, recycling
// intermediate buffers. Order mirrors FreeCCR's export tail.
func (s *Spec) applyGeometry(im *image.Image) *image.Image {
	cur := im
	adv := func(next *image.Image) {
		if next != cur {
			image.PutBuf(cur.Pix)
			cur = next
		}
	}
	if s.Rotation%360 != 0 {
		adv(geometry.Rotate90CW(cur, s.Rotation/90))
	}
	if s.HFlip {
		adv(geometry.FlipH(cur))
	}
	if s.VFlip {
		adv(geometry.FlipV(cur))
	}
	if s.FineRotation != 0 {
		adv(geometry.FineRotate(cur, s.FineRotation))
	}
	if s.HasCrop && !s.CropEditing {
		adv(geometry.Crop(cur, s.Crop))
	}
	return cur
}

// applyLook runs the post-slider tone stages (gamma → curves → cineon), each
// producing a new pooled buffer and recycling the previous one. Inactive stages
// are skipped (no clone).
func (s *Spec) applyLook(im *image.Image) *image.Image {
	cur := im
	if s.Bands != nil && !s.Bands.IsZero() {
		next := adjust.ApplyColorBands(cur, s.Bands)
		image.PutBuf(cur.Pix)
		cur = next
	}
	if s.Gamma != 0 {
		next := adjust.ApplyGamma(cur, s.Gamma, s.GammaLuminance)
		image.PutBuf(cur.Pix)
		cur = next
	}
	if s.Curves != nil && !s.Curves.IsIdentity() {
		next := adjust.ApplyCurves(cur, s.Curves)
		image.PutBuf(cur.Pix)
		cur = next
	}
	if s.Cineon {
		next := adjust.ApplyCineon(cur)
		image.PutBuf(cur.Pix)
		cur = next
	}
	return cur
}

// Job is one input→output unit of batch work.
type Job struct {
	Input   string
	Output  string
	Format  string // "tif" (16-bit), "jpg" (8-bit), or "dng" (linear)
	Quality int    // JPEG quality
}

// Result reports the outcome of a Job.
type Result struct {
	Job  Job
	W, H int
	Err  error
}

// Options tunes the batch run.
type Options struct {
	DecodeWorkers  int  // concurrent decoders (default min(4, NumCPU))
	ProcessWorkers int  // concurrent processors (default NumCPU)
	Preview        bool // decode RAW at half size
}

// RunBatch processes jobs through the decode → process pipeline, invoking
// progress (if non-nil) as each frame completes. It returns per-job results in
// completion order. During the run per-kernel row parallelism is disabled so
// throughput comes from frame-level concurrency (no oversubscription).
func RunBatch(jobs []Job, spec Spec, opt Options, progress func(done, total int, r Result)) []Result {
	ncpu := runtime.GOMAXPROCS(0)
	dw := opt.DecodeWorkers
	if dw <= 0 {
		dw = min(4, ncpu)
	}
	pw := opt.ProcessWorkers
	if pw <= 0 {
		pw = ncpu
	}

	// Frame-level parallelism: run kernels single-threaded per frame.
	prev := par.MaxWorkers()
	par.SetMaxWorkers(1)
	defer par.SetMaxWorkers(prev)

	type decoded struct {
		job Job
		im  *image.Image
		err error
	}
	jobCh := make(chan Job)
	decCh := make(chan decoded, pw)
	resCh := make(chan Result, len(jobs))

	// Feed jobs.
	go func() {
		for _, j := range jobs {
			jobCh <- j
		}
		close(jobCh)
	}()

	// Decode stage.
	var dwg sync.WaitGroup
	for i := 0; i < dw; i++ {
		dwg.Add(1)
		go func() {
			defer dwg.Done()
			for j := range jobCh {
				im, err := decode.Decode(j.Input, opt.Preview)
				decCh <- decoded{job: j, im: im, err: err}
			}
		}()
	}
	go func() { dwg.Wait(); close(decCh) }()

	// Process + encode + write stage.
	var pwg sync.WaitGroup
	for i := 0; i < pw; i++ {
		pwg.Add(1)
		go func() {
			defer pwg.Done()
			s := spec // local copy so each worker's Process is independent
			for d := range decCh {
				res := Result{Job: d.job}
				if d.err != nil {
					res.Err = d.err
					resCh <- res
					continue
				}
				final := s.Process(d.im)
				image.PutBuf(d.im.Pix) // decoded buffer no longer needed
				res.W, res.H = final.W, final.H
				res.Err = export.Write(d.job.Output, final, d.job.Format, d.job.Quality)
				image.PutBuf(final.Pix)
				resCh <- res
			}
		}()
	}
	go func() { pwg.Wait(); close(resCh) }()

	results := make([]Result, 0, len(jobs))
	for r := range resCh {
		results = append(results, r)
		if progress != nil {
			progress(len(results), len(jobs), r)
		}
	}
	return results
}
