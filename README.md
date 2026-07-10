# FreeCCR-go

A high-performance Go port of [FreeCCR](https://github.com/toonoumi/FreeCCR)'s
core image pipeline — color-negative film-scan → positive conversion + color
correction. The original is a Python 3.11 / PySide6 desktop app; this port
reimplements the performance-critical **processing core** in Go with fused,
row-parallel kernels, exposed through a headless engine + CLI (a local web UI is
planned).

## Status

| Phase | Scope | State |
|-------|-------|-------|
| 0 | Skeleton, `Image` type, golden fidelity harness | ✅ done |
| 1 | Core convert + adjust (standard formats) + CLI | ✅ done |
| 2 | RAW decode via cgo + libraw | ✅ done |
| 3 | Batch pipeline + benchmarks + LUT optimization | ✅ done |
| 4 | Local web UI (`freeccrd`) | ✅ done |

**Deferred** (additive on top of the core): dust removal / AI detect,
IT8/DCP/ICC profiling, crop/straighten, Metal GPU.

## Fidelity

Every kernel is validated **against the Python numerically**. `ref/gen_golden.py`
transliterates the exact numpy kernels from `ccr_processor.py` and emits fixtures;
Go tests assert a ≤1-LSB match (most cases bit-exact). Run:

```bash
python3 ref/gen_golden.py             # convert/adjust/curves/gamma fixtures (numpy)
python ref/gen_bands.py <FreeCCR/src> # color-band fixtures (needs the cv2 venv)
go test ./...                         # most cases bit-exact, rest ≤1–2 LSB
```

Curves, gamma, and cineon are pure-numpy LUTs (bit-exact / ≤1 LSB). Color bands
replicate OpenCV's float HSV conversion (FLT_EPSILON + sector table) and are
validated ≤1 LSB against the **real cv2** `apply_color_band_adjustments`.

The port reproduces numpy's semantics deliberately: float32 working values,
`clip→truncate` at uint16 boundaries, RGB channel order (channel 0 = R), and
numpy's percentile interpolation.

Beyond the unit-test golden fixtures, an **A/B harness** (`ref/ab.py`) runs the
**actual FreeCCR Python code** (the real cv2/numpy kernels) and the Go port on
byte-identical scans and diffs the 16-bit outputs. Result: the recommended
**B/W-point linear** conversion is **bit-exact**; density mode is within ≤1 LSB
non-windowed (mean ≪ 1 LSB windowed); auto/reference within a few LSB. Every
residual traces to float32(numpy/cv2) vs float64(Go) arithmetic and is far below
visible. See [`ref/AB.md`](ref/AB.md).

## What's implemented

- **Conversion** (`internal/convert`)
  - Two-point B/W-point inversion — linear + optical-density (log) modes
  - Default-slope (black-point-only) inversion, with optional film-stock slopes
  - Reference/auto percentile normalization + OD alignment + post-invert look
  - Working-space windowing (highlight headroom) + headroom recovery
- **Adjustment** (`internal/adjust`) — the full `adjust_image` slider chain fused
  into one row-parallel pass: white balance, exposure, brightness,
  highlights/shadows, black/white point, contrast, saturation, subtractive
  saturation, per-channel levels. Plus the "look" stages: **monotone-cubic tone
  curves** (per-channel + composite), the **gamma** slider (per-channel +
  hue-preserving luminance mode), **7-band color vectors** (HSV hue/sat/bright/
  subsat with OpenCV-faithful HSV conversion and spatial feather), and the
  **Cineon→Rec.709** transform.
- **Local web UI** (`cmd/freeccrd`) — a Go `net/http` server with an embedded
  single-page app: load a roll → thumbnail strip, click the preview to sample
  the film-base (black) and dense (white) anchors, live convert + adjust with
  every slider, **Sync to All** across the roll, and parallel full-resolution
  **Export All**. All processing is local.
- **I/O** — TIFF/JPEG/PNG decode (`internal/decode`), plus **RAW** (CR2/CR3/NEF/
  ARW/DNG/…) via cgo + libraw, matching FreeCCR's rawpy decode (AHD, 16-bit,
  linear, no auto-bright, Adobe RGB, auto-scaled). Verified **bit-exact** against
  libraw's own `dcraw_emu` (maxAbsDiff=0 over a 40MP frame). Export as 16-bit
  TIFF, 8-bit JPEG, or **linear DNG** (`internal/export`; the DNG imports the
  converted positive into raw editors).

## Build & run

```bash
# Standard formats only (pure Go, no cgo):
go build -o bin/freeccr ./cmd/freeccr        # or: make build

# Full engine with RAW support (needs `brew install libraw`):
make build-raw                                # sets the cgo flags for you

# Two-point B/W conversion (sample film base + dense area from the scan):
bin/freeccr convert scan.tif -o positive.tif \
    --black 61000,58000,52000 --white 9150,8700,7800

# Density mode + adjustments, JPEG out:
bin/freeccr convert scan.tif -o positive.jpg --jpg \
    --black 61000,58000,52000 --white 9150,8700,7800 \
    --density --contrast 20 --saturation 15

# Black-point-only (default slope):
bin/freeccr convert scan.tif -o positive.jpg --jpg --black 61000,58000,52000

# Auto/reference mode (percentile anchors from a reference rectangle):
bin/freeccr convert scan.tif -o positive.jpg --jpg \
    --mode reference --ref 0.0,0.0,0.15,0.2

# RAW input (with a libraw build) works identically:
bin/freeccr convert scan.cr3 -o positive.tif --black 61000,58000,52000

# Export as linear DNG (imports into Lightroom / Camera Raw / RawTherapee):
bin/freeccr convert scan.tif -o positive.dng --black 61000,58000,52000

# Whole roll → one output folder, shared settings, all cores:
bin/freeccr batch ./roll -o ./out --format dng \
    --black 61000,58000,52000 --white 9150,8700,7800 --density --contrast 20

# Passthrough decode (no conversion) — writes the decoded 16-bit TIFF:
bin/freeccr decode scan.dng -o decoded.tif
```

### Web UI

```bash
make serve                    # build (with RAW) + launch
# → open http://127.0.0.1:8422, type a roll folder, Load Roll
```

Load a folder, click a thumbnail, hit **Set Black Pt** and click the clear film
base, optionally **Set White Pt** on a dense area, tune the sliders (live
preview), **Sync to All**, then **Export All**. `freeccrd -dir <folder>` loads a
roll at startup; `-addr` changes the listen address.

`--black`/`--white` are `R,G,B` scan values sampled from the negative (clear film
base = HIGH values, dense/exposed area = LOW values). See `bin/freeccr convert`
with no args for all options.

## Architecture

```
cmd/freeccr        CLI: convert one file / batch a folder / decode
cmd/freeccrd       local web server + embedded SPA (web/)
internal/image     flat float32 interleaved-RGB buffer + sync.Pool
internal/par       goroutine row-tiling; per-frame vs per-row parallelism switch
internal/convert   negative→positive kernels (bwpoint, reference, look, window)
internal/adjust    fused adjust_image slider chain + composed LUTs
internal/decode    TIFF/JPEG/PNG → RGB float32; RAW via cgo+libraw (-tags libraw); resize
internal/export    16-bit TIFF + 8-bit JPEG + linear DNG writers
internal/pipeline  batch decode→process→encode workers; shared roll Spec
internal/session   in-memory roll state + cached previews (web UI)
ref/gen_golden.py  numpy reference → golden fixtures
```

### Performance design
- **Fused pass**: the whole adjustment chain (and the reference-mode
  normalize+invert+look) runs as a single per-pixel pass — one buffer read +
  write instead of numpy's ~10 full-array passes.
- **Composed LUTs**: the pure per-channel scalar stages (white balance, exposure,
  brightness, highlights/shadows, black/white point, contrast) fold into three
  65536-entry LUTs built once per roll — per-pixel `pow`/divides become a table
  lookup. Bit-identical to the per-pixel chain.
- **Two modes of parallelism**: single images tile rows across `GOMAXPROCS`
  (low latency); batch runs kernels single-threaded and parallelizes across
  frames (`internal/pipeline`, decode→process→encode workers with `sync.Pool`
  buffer reuse) — no GIL, no oversubscription.

### Benchmarks

Compute core, 2000×1333 (2.67 MP), this 14-core Mac. numpy is the exact
FreeCCR reference math (`ref/bench_np.py`), SIMD-vectorized, ~1 core:

| Operation | numpy (1 core) | Go (14 core) | speedup |
|-----------|---------------:|-------------:|--------:|
| adjust_image (full chain) | 304 ms | **41 ms** | 7.4× |
| two-point density invert  | 23.5 ms | **15 ms** | 1.6× |
| reference normalize+look  | 113 ms | **37 ms** | 3.1× |

Batch: a 24-frame roll (2.67 MP, density convert + 5 adjustments) → JPEG in
~0.74 s (≈32 frames/s) end-to-end incl. decode/encode. Reproduce with
`make golden && (cd ref && python3 bench_np.py)` and
`go test -bench . ./internal/adjust ./internal/convert`.

## License

The upstream FreeCCR is AGPL-3.0; this port inherits those terms.
