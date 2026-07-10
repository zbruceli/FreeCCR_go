# A/B: FreeCCR-go vs the real FreeCCR Python pipeline

This harness runs a battery of conversion + adjustment cases through **both** the
Go port and the **actual** FreeCCR Python code (`src/core/ccr_processor.py`, the
real cv2/numpy kernels — not the numpy transliteration used for the unit-test
golden fixtures), on **byte-identical input pixels**, and diffs the 16-bit
outputs. It is an independent, end-to-end check that the port reproduces the
original.

## Setup

```bash
python3 -m venv .venv && . .venv/bin/activate
pip install "numpy<2" opencv-python-headless tifffile      # the real FreeCCR deps
go build -o bin/freeccr ./cmd/freeccr
go build -o bin/tifdiff ./tools/tifdiff

python ref/ab.py <scan.tif> \
    --src /path/to/FreeCCR/src --go ./bin/freeccr --tifdiff ./bin/tifdiff \
    --black R,G,B --white R,G,B [--ref x0,y0,x1,y1]
```

`ab_run.py` drives the real `apply_bwpoint_normalization` / `adjust_image` /
`compute_reference_norm_params` / `apply_reference_normalization`. Drop your own
16-bit negative scans in to A/B on real film.

## Input-stack parity (prerequisite)

Go's TIFF I/O (`x/image`) and Python's (`tifffile`) were verified to read/write
byte-identical `uint16` RGB (Go→Py→Go round-trip: **maxAbsDiff = 0**), so the
input pixels entering both pipelines are the same and any output difference is
pure pipeline math.

## Results (max / mean absolute diff in 16-bit LSB)

Real RAW-decoded photograph, 1600×1198, anchors from its own histogram:

| Case | max LSB | mean LSB | % pixels differ |
|------|--------:|---------:|----------------:|
| two-point **linear**, WS on        | **0**  | 0.0000 | 0.000 |
| two-point density, WS **off**      | 1      | 0.0008 | 0.078 |
| two-point density, WS on           | 64     | 0.0018 | 0.003 |
| two-point density, WS on, +adjust  | 72     | 0.0062 | 0.398 |
| default-slope, WS on               | 64     | 0.0088 | 0.014 |
| reference / auto                   | 49     | 1.5    | 71    |

## Analysis — every gap is float32(numpy/cv2) vs float64(Go)

The port is **bit-exact** where the math is pure arithmetic, and diverges only
in float precision where numpy/cv2 use float32 and Go uses float64:

- **Two-point linear inversion is bit-exact (0 LSB), even windowed** — it is
  affine arithmetic with no transcendentals, so the working-space windowing
  itself is proven exact.
- **Density mode, non-windowed: ≤1 LSB** — `log10` in float64 (Go) vs float32
  (numpy) rounds a handful of pixels differently.
- **Density mode, windowed: mean < 0.01 LSB, but rare pixels up to ~64 LSB.**
  A single-LSB difference in the *windowed base* is amplified ~64× when the
  display range is recovered (window width 1024 → 65535, ×64). Fewer than 0.4%
  of pixels are affected; the image is visually identical.
- **Reference / auto: the apply math matches to ≤2 LSB given identical params**
  (verified by feeding numpy's params to the Go apply). The remaining spread is
  the *params*: numpy computes the optical-density-mean reduction (and log10) in
  float32, Go in float64, so `od_factors` shift slightly and the global stretch
  spreads that over the frame (mean ~1.5 LSB on real content; more on smooth
  synthetic gradients, which are pathologically sensitive to the anchors).

None of these is a logic difference — bit-matching numpy/cv2's float32
transcendentals and reductions from Go is impractical and the residual (mean
≪ 1 LSB for the recommended B/W-point path; a few LSB for auto) is far below
visible. The recommended **B/W-point linear** workflow is bit-exact.
