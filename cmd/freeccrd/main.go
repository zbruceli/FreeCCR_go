// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Command freeccrd serves the FreeCCR-go local web UI: load a roll, sample B/W
// points, convert + adjust with a live preview, sync across the roll, and export
// at full resolution. All processing is local; the SPA is embedded.
package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	stdimg "image"
	"image/jpeg"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/zhengli/freeccr-go/internal/adjust"
	"github.com/zhengli/freeccr-go/internal/decode"
	"github.com/zhengli/freeccr-go/internal/export"
	"github.com/zhengli/freeccr-go/internal/image"
	"github.com/zhengli/freeccr-go/internal/par"
	"github.com/zhengli/freeccr-go/internal/pipeline"
	"github.com/zhengli/freeccr-go/internal/session"
)

//go:embed web
var webFS embed.FS

type server struct {
	sess *session.Session
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8422", "listen address")
	open := flag.String("dir", "", "optional roll directory to load at startup")
	flag.Parse()

	s := &server{sess: session.New()}
	if *open != "" {
		if _, err := s.sess.Load(*open, nil); err != nil {
			log.Printf("initial load %s: %v", *open, err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/browse", s.handleBrowse)
	mux.HandleFunc("/api/load", s.handleLoad)
	mux.HandleFunc("/api/thumb", s.handleThumb)
	mux.HandleFunc("/api/preview", s.handlePreview)
	mux.HandleFunc("/api/sample", s.handleSample)
	mux.HandleFunc("/api/export", s.handleExport)

	fmt.Printf("FreeCCR-go UI → http://%s\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

// --- request/response DTOs ------------------------------------------------

type adjDTO struct {
	Exposure, Brightness, Contrast, Saturation float64
	Temp, Tint                                 float64
	Highlights, Shadows                        float64
	Blackpoint, Whitepoint, SubSat             float64
	// Per-channel Levels
	ChInputGain, ChMasterShift, ChMasterGain float64
	ChRShift, ChRGain, ChRBlackpoint         float64
	ChGShift, ChGGain, ChGBlackpoint         float64
	ChBShift, ChBGain, ChBBlackpoint         float64
}

type curvesDTO struct {
	RGB [][2]float64 `json:"rgb"`
	R   [][2]float64 `json:"r"`
	G   [][2]float64 `json:"g"`
	B   [][2]float64 `json:"b"`
}

type specReq struct {
	ID       int           `json:"id"`
	Mode     string        `json:"mode"`
	Black    [3]float64    `json:"black"`
	White    [3]float64    `json:"white"`
	HasWhite bool          `json:"hasWhite"`
	Density  bool          `json:"density"`
	Ref      [4]float64    `json:"ref"`
	HasRef   bool          `json:"hasRef"`
	WS       bool          `json:"ws"`
	Adj      adjDTO        `json:"adj"`
	Gamma    float64       `json:"gamma"`
	GammaLum bool          `json:"gammaLum"`
	Cineon   bool          `json:"cineon"`
	Bands    [7][4]float64 `json:"bands"`
	Feather  float64       `json:"feather"`
	Curves   curvesDTO     `json:"curves"`
}

func (r *specReq) toSpec() pipeline.Spec {
	sp := pipeline.Spec{WS: r.WS}
	p := adjust.DefaultParams()
	a := r.Adj
	p.Exposure, p.Brightness, p.Contrast, p.Saturation = a.Exposure, a.Brightness, a.Contrast, a.Saturation
	p.Kelvin, p.Tint = a.Temp, a.Tint
	p.Highlights, p.Shadows = a.Highlights, a.Shadows
	p.Blackpoint, p.Whitepoint, p.SubSaturation = a.Blackpoint, a.Whitepoint, a.SubSat
	p.ChInputGain, p.ChMasterShift, p.ChMasterGain = a.ChInputGain, a.ChMasterShift, a.ChMasterGain
	p.ChRShift, p.ChRGain, p.ChRBlackpoint = a.ChRShift, a.ChRGain, a.ChRBlackpoint
	p.ChGShift, p.ChGGain, p.ChGBlackpoint = a.ChGShift, a.ChGGain, a.ChGBlackpoint
	p.ChBShift, p.ChBGain, p.ChBBlackpoint = a.ChBShift, a.ChBGain, a.ChBBlackpoint
	sp.Adjust = p

	if r.Mode == "reference" {
		sp.Mode = pipeline.ModeReference
		sp.RefRect = r.Ref
		sp.HasRefRect = r.HasRef
	} else {
		sp.Mode = pipeline.ModeBWPoint
		sp.Black = r.Black
		sp.HasWhite = r.HasWhite
		sp.White = r.White
		sp.Density = r.Density
	}

	// Look stages.
	sp.Gamma, sp.GammaLuminance, sp.Cineon = r.Gamma, r.GammaLum, r.Cineon
	bs := &adjust.BandSettings{Bands: r.Bands, Feather: r.Feather}
	if !bs.IsZero() {
		sp.Bands = bs
	}
	cv := &adjust.Curves{RGB: r.Curves.RGB, R: r.Curves.R, G: r.Curves.G, B: r.Curves.B}
	if !cv.IsIdentity() {
		sp.Curves = cv
	}
	return sp
}

// --- handlers -------------------------------------------------------------

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}

// handleBrowse lists sub-directories of a path so the SPA can offer a
// filesystem folder picker (the server runs on the user's own machine). It also
// reports how many supported images each folder directly contains, so rolls are
// easy to spot. Empty/invalid path falls back to the home directory.
func (s *server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	home, _ := os.UserHomeDir()
	p := r.URL.Query().Get("path")
	if p == "" {
		p = home
	}
	p = filepath.Clean(p)
	entries, err := os.ReadDir(p)
	if err != nil {
		// Fall back to home on a bad path rather than erroring the UI.
		p = home
		entries, err = os.ReadDir(p)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	type dirent struct {
		Name   string `json:"name"`
		Images int    `json:"images"`
	}
	var dirs []dirent
	for _, e := range entries {
		if !e.IsDir() || len(e.Name()) == 0 || e.Name()[0] == '.' {
			continue
		}
		dirs = append(dirs, dirent{Name: e.Name(), Images: countImages(filepath.Join(p, e.Name()))})
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	writeJSON(w, map[string]any{
		"path":   p,
		"parent": filepath.Dir(p),
		"home":   home,
		"images": countImages(p), // images directly in the current folder
		"dirs":   dirs,
	})
}

// countImages returns the number of supported image files directly in dir
// (bounded; used only for the picker hint).
func countImages(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if decode.IsRAW(e.Name()) || decode.IsStandard(e.Name()) {
			n++
		}
	}
	return n
}

func (s *server) handleLoad(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	frames, err := s.sess.Load(req.Dir, nil)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	type fr struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		W    int    `json:"w"`
		H    int    `json:"h"`
		OK   bool   `json:"ok"`
	}
	out := make([]fr, len(frames))
	for i, f := range frames {
		out[i] = fr{ID: f.ID, Name: f.Name, W: f.PW, H: f.PH, OK: f.Preview != nil}
	}
	writeJSON(w, map[string]any{
		"dir":    s.sess.Dir(),
		"outDir": s.sess.DefaultOutDir(),
		"frames": out,
	})
}

func (s *server) handleThumb(w http.ResponseWriter, r *http.Request) {
	f := s.sess.Frame(atoi(r.URL.Query().Get("id"), -1))
	if f == nil || f.Preview == nil {
		http.Error(w, "no such frame", 404)
		return
	}
	thumb := decode.ResizeToMaxSide(f.Preview, 200)
	writeJPEG(w, thumb, 80)
	if thumb != f.Preview {
		image.PutBuf(thumb.Pix)
	}
}

func (s *server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req specReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	f := s.sess.Frame(req.ID)
	if f == nil || f.Preview == nil {
		http.Error(w, "no such frame", 404)
		return
	}
	// Before any film-base is sampled, a B/W-point conversion has no anchor and
	// would render black. Show the raw scan instead so the user can see it and
	// sample the clear base / dense point. (Reference/auto mode always has the
	// whole-frame default, so it converts fine.)
	if req.Mode != "reference" && !req.HasWhite && req.Black == ([3]float64{0, 0, 0}) {
		writeJPEG(w, f.Preview, 88)
		return
	}
	sp := req.toSpec()
	out := sp.Process(f.Preview)
	writeJPEG(w, out, 88)
	image.PutBuf(out.Pix)
}

// handleSample returns the average scan RGB over a small patch around the
// normalized (x,y) click on the preview — used to set B/W anchors.
func (s *server) handleSample(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := s.sess.Frame(atoi(q.Get("id"), -1))
	if f == nil || f.Preview == nil {
		http.Error(w, "no such frame", 404)
		return
	}
	nx, _ := strconv.ParseFloat(q.Get("x"), 64)
	ny, _ := strconv.ParseFloat(q.Get("y"), 64)
	im := f.Preview
	cx := int(nx * float64(im.W))
	cy := int(ny * float64(im.H))
	const rad = 3
	var sr, sg, sb float64
	n := 0
	for yy := cy - rad; yy <= cy+rad; yy++ {
		if yy < 0 || yy >= im.H {
			continue
		}
		for xx := cx - rad; xx <= cx+rad; xx++ {
			if xx < 0 || xx >= im.W {
				continue
			}
			o := (yy*im.W + xx) * 3
			sr += float64(im.Pix[o])
			sg += float64(im.Pix[o+1])
			sb += float64(im.Pix[o+2])
			n++
		}
	}
	if n == 0 {
		http.Error(w, "out of bounds", 400)
		return
	}
	writeJSON(w, map[string]float64{
		"r": sr / float64(n), "g": sg / float64(n), "b": sb / float64(n),
	})
}

func (s *server) handleExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OutDir  string    `json:"outDir"`
		Format  string    `json:"format"` // "tif" | "jpg" | "dng"
		Quality int       `json:"quality"`
		Frames  []specReq `json:"frames"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.OutDir == "" {
		req.OutDir = s.sess.DefaultOutDir()
	}
	if err := os.MkdirAll(req.OutDir, 0o755); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.Quality == 0 {
		req.Quality = 95
	}
	if req.Format == "" {
		req.Format = "tif"
	}
	ext := export.Ext(req.Format)

	// Full-res decode + process per frame, parallel across frames (kernels
	// single-threaded, like the batch pipeline).
	prev := par.MaxWorkers()
	par.SetMaxWorkers(1)
	defer par.SetMaxWorkers(prev)

	start := time.Now()
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var mu sync.Mutex
	var ok, failed int
	var firstErr string
	for _, fr := range req.Frames {
		frame := s.sess.Frame(fr.ID)
		if frame == nil {
			continue
		}
		wg.Add(1)
		go func(fr specReq, path, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			im, err := decode.Decode(path, false)
			if err == nil {
				sp := fr.toSpec()
				final := sp.Process(im)
				image.PutBuf(im.Pix)
				out := filepath.Join(req.OutDir, session.TrimExt(name)+ext)
				err = export.Write(out, final, req.Format, req.Quality)
				image.PutBuf(final.Pix)
			}
			mu.Lock()
			if err != nil {
				failed++
				if firstErr == "" {
					firstErr = err.Error()
				}
			} else {
				ok++
			}
			mu.Unlock()
		}(fr, frame.Path, frame.Name)
	}
	wg.Wait()
	writeJSON(w, map[string]any{
		"ok": ok, "failed": failed, "outDir": req.OutDir,
		"seconds": time.Since(start).Seconds(), "error": firstErr,
	})
}

// --- helpers --------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeJPEG(w http.ResponseWriter, im *image.Image, q int) {
	rgba := stdimg.NewRGBA(stdimg.Rect(0, 0, im.W, im.H))
	for i, n := 0, im.W*im.H; i < n; i++ {
		rgba.Pix[i*4] = to8(im.Pix[i*3])
		rgba.Pix[i*4+1] = to8(im.Pix[i*3+1])
		rgba.Pix[i*4+2] = to8(im.Pix[i*3+2])
		rgba.Pix[i*4+3] = 255
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: q})
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(buf.Bytes())
}

func to8(v float32) uint8 {
	u := int(float64(v)*(255.0/65535.0) + 0.5)
	if u < 0 {
		return 0
	}
	if u > 255 {
		return 255
	}
	return uint8(u)
}

func atoi(s string, def int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return def
}
