// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Package par provides simple goroutine row-tiling for per-pixel kernels.
package par

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// maxWorkers caps the goroutines a single Rows call spawns. It defaults to
// GOMAXPROCS (row-level parallelism, best for single-image latency). Batch
// processing sets it to 1 so kernels run single-threaded and parallelism comes
// from processing many frames at once (frame-level), avoiding oversubscription.
var maxWorkers atomic.Int64

func init() { maxWorkers.Store(int64(runtime.GOMAXPROCS(0))) }

// SetMaxWorkers sets the per-call worker cap (min 1). Global; intended to be set
// once at startup or around a batch run.
func SetMaxWorkers(n int) {
	if n < 1 {
		n = 1
	}
	maxWorkers.Store(int64(n))
}

// MaxWorkers returns the current per-call worker cap.
func MaxWorkers() int { return int(maxWorkers.Load()) }

// Rows splits the half-open range [0, n) across up to MaxWorkers() workers and
// invokes fn(lo, hi) on disjoint contiguous sub-ranges. It blocks until all
// workers finish. For small n or a cap of 1 it runs inline.
func Rows(n int, fn func(lo, hi int)) {
	if n <= 0 {
		return
	}
	workers := int(maxWorkers.Load())
	// Row counts below this don't amortize goroutine scheduling.
	if workers <= 1 || n < 64 {
		fn(0, n)
		return
	}
	if workers > n {
		workers = n
	}
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for lo := 0; lo < n; lo += chunk {
		hi := lo + chunk
		if hi > n {
			hi = n
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			fn(lo, hi)
		}(lo, hi)
	}
	wg.Wait()
}
