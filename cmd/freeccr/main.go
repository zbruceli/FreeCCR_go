// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Command freeccr is the headless FreeCCR-go engine CLI: decode → negative
// conversion → color adjustment → export, for a single file or a whole folder.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zhengli/freeccr-go/internal/adjust"
	"github.com/zhengli/freeccr-go/internal/decode"
	"github.com/zhengli/freeccr-go/internal/export"
	"github.com/zhengli/freeccr-go/internal/pipeline"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "convert":
		runConvert(os.Args[2:])
	case "batch":
		runBatch(os.Args[2:])
	case "decode":
		runDecode(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

// specFlags registers all conversion + adjustment flags on fs and returns a
// closure that builds a pipeline.Spec from the parsed values. Shared by the
// convert and batch subcommands so their options stay identical.
func specFlags(fs *flag.FlagSet) func() (pipeline.Spec, error) {
	mode := fs.String("mode", "bwpoint", "conversion mode: bwpoint | reference")
	blackS := fs.String("black", "", "clear/film-base sample R,G,B (HIGH values) — bwpoint")
	whiteS := fs.String("white", "", "dense/exposed sample R,G,B (LOW values); omit for default-slope")
	refS := fs.String("ref", "", "reference rect x0,y0,x1,y1 (normalized 0..1) — reference mode")
	density := fs.Bool("density", false, "bwpoint: recover optical density (log space) instead of linear")
	noWS := fs.Bool("no-ws", false, "disable working-space windowing")

	exposure := fs.Float64("exposure", 0, "exposure (-200..200)")
	brightness := fs.Float64("brightness", 0, "brightness (-100..100)")
	contrast := fs.Float64("contrast", 0, "contrast (-100..100)")
	saturation := fs.Float64("saturation", 0, "saturation (-100..100)")
	kelvin := fs.Float64("temp", 0, "temperature (-100..100)")
	tint := fs.Float64("tint", 0, "tint (-100..100)")
	highlights := fs.Float64("highlights", 0, "highlights (-100..100)")
	shadows := fs.Float64("shadows", 0, "shadows (-100..100)")
	blackpoint := fs.Float64("blackpoint", 0, "black point (-100..100)")
	whitepoint := fs.Float64("whitepoint", 0, "white point (-100..100)")
	subSat := fs.Float64("subsat", 0, "subtractive saturation (-100..100)")

	return func() (pipeline.Spec, error) {
		s := pipeline.Spec{WS: !*noWS}
		p := adjust.DefaultParams()
		p.Exposure, p.Brightness, p.Contrast, p.Saturation = *exposure, *brightness, *contrast, *saturation
		p.Kelvin, p.Tint = *kelvin, *tint
		p.Highlights, p.Shadows = *highlights, *shadows
		p.Blackpoint, p.Whitepoint, p.SubSaturation = *blackpoint, *whitepoint, *subSat
		s.Adjust = p

		switch *mode {
		case "bwpoint":
			black, err := parseN(*blackS, 3)
			if err != nil {
				return s, fmt.Errorf("--black: %w", err)
			}
			s.Mode = pipeline.ModeBWPoint
			s.Black = [3]float64{black[0], black[1], black[2]}
			if *whiteS != "" {
				white, err := parseN(*whiteS, 3)
				if err != nil {
					return s, fmt.Errorf("--white: %w", err)
				}
				s.HasWhite = true
				s.White = [3]float64{white[0], white[1], white[2]}
				s.Density = *density
			}
		case "reference":
			rect, err := parseN(*refS, 4)
			if err != nil {
				return s, fmt.Errorf("--ref: %w", err)
			}
			s.Mode = pipeline.ModeReference
			s.RefRect = [4]float64{rect[0], rect[1], rect[2], rect[3]}
			s.HasRefRect = true
		default:
			return s, fmt.Errorf("unknown --mode %q", *mode)
		}
		return s, nil
	}
}

func runConvert(args []string) {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		usage()
		os.Exit(2)
	}
	input := args[0]
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	out := fs.String("o", "", "output path (.tif or .jpg); required")
	jpg := fs.Bool("jpg", false, "write 8-bit JPEG instead of 16-bit TIFF")
	quality := fs.Int("quality", 95, "JPEG quality (1-100)")
	build := specFlags(fs)
	_ = fs.Parse(args[1:])
	if *out == "" {
		usage()
		os.Exit(2)
	}
	spec, err := build()
	if err != nil {
		fatalf("%v", err)
	}

	start := time.Now()
	im, err := decode.Decode(input, false)
	if err != nil {
		fatalf("%v", err)
	}
	tDecode := time.Now()
	final := spec.Process(im)
	tProc := time.Now()
	if *jpg {
		err = export.WriteJPEG(*out, final, *quality)
	} else {
		err = export.WriteTIFF16(*out, final)
	}
	if err != nil {
		fatalf("write %s: %v", *out, err)
	}
	tWrite := time.Now()
	fmt.Printf("OK  %s  (%dx%d)\n", *out, final.W, final.H)
	fmt.Printf("    decode %6.1fms  process %6.1fms  write %6.1fms  total %6.1fms\n",
		ms(start, tDecode), ms(tDecode, tProc), ms(tProc, tWrite), ms(start, tWrite))
}

func runBatch(args []string) {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		usage()
		os.Exit(2)
	}
	inDir := args[0]
	fs := flag.NewFlagSet("batch", flag.ExitOnError)
	outDir := fs.String("o", "", "output directory; required")
	jpg := fs.Bool("jpg", false, "write 8-bit JPEG instead of 16-bit TIFF")
	quality := fs.Int("quality", 95, "JPEG quality (1-100)")
	preview := fs.Bool("preview", false, "decode RAW at half size")
	decW := fs.Int("decode-workers", 0, "concurrent decoders (0=auto)")
	procW := fs.Int("process-workers", 0, "concurrent processors (0=auto)")
	build := specFlags(fs)
	_ = fs.Parse(args[1:])
	if *outDir == "" {
		usage()
		os.Exit(2)
	}
	spec, err := build()
	if err != nil {
		fatalf("%v", err)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatalf("mkdir %s: %v", *outDir, err)
	}

	ext := ".tif"
	if *jpg {
		ext = ".jpg"
	}
	jobs, err := buildJobs(inDir, *outDir, ext, *jpg, *quality)
	if err != nil {
		fatalf("%v", err)
	}
	if len(jobs) == 0 {
		fatalf("no supported images found in %s", inDir)
	}

	fmt.Printf("Processing %d frames  (%v decode / %v process workers)...\n",
		len(jobs), workersOr(*decW, "auto"), workersOr(*procW, "auto"))
	start := time.Now()
	var nErr int
	results := pipeline.RunBatch(jobs, spec, pipeline.Options{
		DecodeWorkers: *decW, ProcessWorkers: *procW, Preview: *preview,
	}, func(done, total int, r pipeline.Result) {
		if r.Err != nil {
			nErr++
			fmt.Printf("  [%d/%d] ERROR %s: %v\n", done, total, filepath.Base(r.Job.Input), r.Err)
		} else {
			fmt.Printf("  [%d/%d] %s (%dx%d)\n", done, total, filepath.Base(r.Job.Output), r.W, r.H)
		}
	})
	elapsed := time.Since(start)
	ok := len(results) - nErr
	fmt.Printf("\nDone: %d ok, %d failed in %.2fs  (%.1f ms/frame, %.2f frames/s)\n",
		ok, nErr, elapsed.Seconds(),
		float64(elapsed.Milliseconds())/float64(max(1, len(results))),
		float64(ok)/elapsed.Seconds())
	if nErr > 0 {
		os.Exit(1)
	}
}

func buildJobs(inDir, outDir, ext string, jpg bool, quality int) ([]pipeline.Job, error) {
	entries, err := os.ReadDir(inDir)
	if err != nil {
		return nil, err
	}
	var jobs []pipeline.Job
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !decode.IsRAW(name) && !decode.IsStandard(name) {
			continue
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		jobs = append(jobs, pipeline.Job{
			Input:   filepath.Join(inDir, name),
			Output:  filepath.Join(outDir, base+ext),
			JPEG:    jpg,
			Quality: quality,
		})
	}
	return jobs, nil
}

// runDecode decodes a file (RAW or standard) and writes it verbatim as a 16-bit
// TIFF — no conversion. Used to cross-validate the libraw binding.
func runDecode(args []string) {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		usage()
		os.Exit(2)
	}
	input := args[0]
	fs := flag.NewFlagSet("decode", flag.ExitOnError)
	out := fs.String("o", "", "output .tif (16-bit); required")
	half := fs.Bool("half", false, "decode RAW at half size (preview)")
	_ = fs.Parse(args[1:])
	if *out == "" {
		fatalf("decode: -o <output.tif> required")
	}
	start := time.Now()
	im, err := decode.Decode(input, *half)
	if err != nil {
		fatalf("%v", err)
	}
	dec := time.Now()
	if err := export.WriteTIFF16(*out, im); err != nil {
		fatalf("write %s: %v", *out, err)
	}
	fmt.Printf("OK  %s  (%dx%d)  decode %.1fms  write %.1fms\n",
		*out, im.W, im.H, ms(start, dec), ms(dec, time.Now()))
}

func parseN(s string, n int) ([]float64, error) {
	parts := strings.Split(s, ",")
	if len(parts) != n {
		return nil, fmt.Errorf("want %d comma-separated values, got %q", n, s)
	}
	out := make([]float64, n)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func workersOr(n int, auto string) any {
	if n <= 0 {
		return auto
	}
	return n
}

func ms(a, b time.Time) float64 { return float64(b.Sub(a).Microseconds()) / 1000.0 }

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "freeccr: "+format+"\n", a...)
	os.Exit(1)
}

func usage() {
	fmt.Fprint(os.Stderr, `freeccr — FreeCCR-go engine

Usage:
  freeccr convert <input> -o <output> [options]     one file
  freeccr batch   <indir> -o <outdir> [options]     whole folder (roll)
  freeccr decode  <input> -o <output.tif> [--half]  passthrough decode (no convert)

Modes (convert / batch):
  --mode bwpoint   (default) two-point B/W inversion.
     --black R,G,B   clear/film-base sample (HIGH scan values), required
     --white R,G,B   dense/exposed sample (LOW values); omit → default-slope
     --density       recover optical density in log space (two-point only)
  --mode reference  auto percentile normalization + post-invert look
     --ref x0,y0,x1,y1   reference rectangle, normalized 0..1

Output:
  -o <path/dir>    .tif (16-bit) or .jpg; a directory for batch
  --jpg            write JPEG; --quality N (1-100)
  --no-ws          disable working-space windowing

Batch:
  --preview        decode RAW at half size
  --decode-workers N / --process-workers N   (0 = auto)

Adjustments (all default 0):
  --exposure --brightness --contrast --saturation --temp --tint
  --highlights --shadows --blackpoint --whitepoint --subsat
`)
}
