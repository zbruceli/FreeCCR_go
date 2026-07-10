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
}

// Process runs the full conversion + adjustment chain on one decoded image and
// returns the final quantized image. The input buffer is left untouched (the
// caller owns it); intermediate buffers are recycled to the pool.
func (s *Spec) Process(im *image.Image) *image.Image {
	ws := s.WS
	var conv *image.Image
	switch s.Mode {
	case ModeReference:
		params := s.Ref
		if params == nil {
			// Per-frame auto: derive params from this frame's reference rect.
			crop := im.CropNormRect(s.RefRect[0], s.RefRect[1], s.RefRect[2], s.RefRect[3])
			p := convert.ComputeReferenceNormParams(crop)
			image.PutBuf(crop.Pix)
			params = &p
		}
		conv = convert.ApplyReferenceNormalization(im, *params)
		ws = false // reference path applies its own inversion + look, not windowed
	default:
		if s.HasWhite {
			conv = convert.TwoPointInvert(im, s.Black, s.White, s.Density, ws)
		} else {
			conv = convert.DefaultSlopeInvert(im, s.Black, s.Slopes, ws)
		}
	}
	final := adjust.AdjustImage(conv, s.Adjust, ws)
	image.PutBuf(conv.Pix)
	return final
}

// Job is one input→output unit of batch work.
type Job struct {
	Input   string
	Output  string
	JPEG    bool
	Quality int
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
				if d.job.JPEG {
					res.Err = export.WriteJPEG(d.job.Output, final, d.job.Quality)
				} else {
					res.Err = export.WriteTIFF16(d.job.Output, final)
				}
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
