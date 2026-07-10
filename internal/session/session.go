// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Package session holds the in-memory state of a loaded roll for the web UI:
// the frame list plus cached preview-size decodes for fast interactive
// processing. Full-resolution frames are decoded on demand for export.
package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/zhengli/freeccr-go/internal/decode"
	"github.com/zhengli/freeccr-go/internal/image"
)

// PreviewMaxSide is the longest side of a cached preview.
const PreviewMaxSide = 1080

// Frame is one loaded image.
type Frame struct {
	ID      int
	Name    string
	Path    string
	Preview *image.Image // decoded, downscaled to <= PreviewMaxSide
	PW, PH  int
}

// Session is a loaded roll. Safe for concurrent use.
type Session struct {
	mu     sync.RWMutex
	dir    string
	frames []*Frame
}

// New returns an empty session.
func New() *Session { return &Session{} }

// Dir returns the currently loaded directory.
func (s *Session) Dir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dir
}

// Load decodes every supported image in dir at preview size, replacing any
// previously loaded roll. Decoding runs concurrently; frames are returned
// sorted by filename. progress (if non-nil) is called after each frame.
func (s *Session) Load(dir string, progress func(done, total int)) ([]*Frame, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if decode.IsRAW(e.Name()) || decode.IsStandard(e.Name()) {
			paths = append(paths, e.Name())
		}
	}
	sort.Strings(paths)

	frames := make([]*Frame, len(paths))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // bound concurrent decodes
	var done int
	var pmu sync.Mutex
	for i, name := range paths {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			f := &Frame{ID: i, Name: name, Path: filepath.Join(dir, name)}
			im, derr := decode.Decode(f.Path, true) // preview=half-size for RAW
			if derr == nil {
				pv := decode.ResizeToMaxSide(im, PreviewMaxSide)
				if pv != im {
					image.PutBuf(im.Pix)
				}
				f.Preview, f.PW, f.PH = pv, pv.W, pv.H
			}
			frames[i] = f
			pmu.Lock()
			done++
			if progress != nil {
				progress(done, len(paths))
			}
			pmu.Unlock()
		}(i, name)
	}
	wg.Wait()

	s.mu.Lock()
	s.dir = dir
	s.frames = frames
	s.mu.Unlock()
	return frames, nil
}

// Frames returns the loaded frames.
func (s *Session) Frames() []*Frame {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.frames
}

// Frame returns the frame with the given ID, or nil.
func (s *Session) Frame(id int) *Frame {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if id < 0 || id >= len(s.frames) {
		return nil
	}
	return s.frames[id]
}

// DefaultOutDir suggests an export directory next to the loaded roll.
func (s *Session) DefaultOutDir() string {
	d := s.Dir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "freeccr_export")
}

// TrimExt returns name without its extension.
func TrimExt(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}
