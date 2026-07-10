# FreeCCR-go

A fast, cross-platform Go port of [FreeCCR](https://github.com/toonoumi/FreeCCR) —
turn color-negative film scans into positives with a physics-based conversion
(not a naive `255 − value` invert) and a full color-correction suite. Ships as a
**native desktop app** (macOS / Windows / Linux), a **local web UI**, and a
scriptable **CLI**. RAW-native, and validated **bit-exact** against the original
Python.

## Features

- **Conversion** — two-point B/W-point (linear + optical-density log), black-point-only default-slope, and auto/reference percentile modes, with working-space highlight headroom.
- **Color correction** — white balance / temp / tint, exposure, brightness, highlights & shadows, black/white point, contrast, saturation, subtractive saturation, and per-channel R/G/B levels.
- **Look** — monotone-cubic tone **curves** (composite + per-channel), a **gamma** slider (per-channel or hue-preserving), **7-band HSV color vectors**, and a **Cineon → Rec.709** transform.
- **Analysis** — live RGB **histogram**, RGB **parade** waveform + **vectorscope**, and a hover pixel probe.
- **Geometry & view** — crop, straighten, 90° rotate, H/V flip, zoom & pan.
- **Assisted** — auto-white-balance (4 algorithms), auto-exposure, and a WB eyedropper.
- **Roll workflow** — batch convert/export, **Sync to All** (by group), and copy/paste adjustments across frames.
- **I/O** — RAW (CR2/CR3/NEF/ARW/DNG/…) via libraw plus TIFF/JPEG/PNG in; 16-bit TIFF, 8-bit JPEG, or **linear DNG** out.

## Install & run

### Desktop app

Prebuilt macOS / Windows / Linux binaries are produced by CI — grab them from the
latest [Actions run](https://github.com/zbruceli/FreeCCR_go/actions) (or a tagged
release). To build the macOS app locally:

```bash
brew install libraw          # RAW support; Xcode CLT provides the webview
make run-app                 # → bin/FreeCCR-go.app, then launches it
```

The app is code-signed with your keychain's *Developer ID Application* identity if
present (else ad-hoc). For distribution, notarize once:
`NOTARY_PROFILE=<profile> make app` (after `xcrun notarytool store-credentials`).
Linux needs `libgtk-3` + `webkit2gtk`; Windows uses the built-in WebView2.

### CLI

```bash
make build-raw               # bin/freeccr with RAW (needs libraw); or `make build` (no RAW)

# Two-point B/W conversion — sample the clear film base and a dense area:
bin/freeccr convert scan.cr3 -o positive.tif \
    --black 61000,58000,52000 --white 9150,8700,7800 --density

# Adjustments + JPEG (or .dng / .tif — format follows the extension):
bin/freeccr convert scan.tif -o positive.jpg --black 61000,58000,52000 \
    --contrast 20 --saturation 15

# Whole roll → one folder, shared settings, all cores:
bin/freeccr batch ./roll -o ./out --format dng \
    --black 61000,58000,52000 --white 9150,8700,7800 --density
```

`--black`/`--white` are `R,G,B` scan values (clear film base = HIGH, dense area =
LOW). Run `bin/freeccr convert` with no args for all options.

### Web UI

```bash
make serve                   # → http://127.0.0.1:8422
```

Load a folder, click a thumbnail, **Set Black Pt** on the clear film base
(optionally **Set White Pt** on a dense area), tune with live preview, **Sync to
All**, then **Export All**.

## Fidelity

Every conversion and color kernel is validated numerically against the **actual
FreeCCR Python code** (the real numpy/cv2 math) on byte-identical pixels: the
B/W-point linear path is **bit-exact**, everything else lands within a few LSB,
and every residual traces to float32 (numpy/cv2) vs float64 (Go) arithmetic — far
below visible. RAW decode is bit-exact against libraw's own `dcraw_emu` over a
40 MP frame. Details in [`ref/AB.md`](ref/AB.md); regenerate/check with
`go test ./...`.

## Performance

Fused single-pass kernels, per-channel LUTs built once per roll, and goroutine
parallelism (no GIL) make the core several times faster than the numpy original.
Compute core at 2000×1333 (2.67 MP) on a 14-core Mac, vs the exact numpy math:

| Operation | numpy (1 core) | Go (14 core) | speedup |
|---|---:|---:|---:|
| adjust chain (all stages) | 304 ms | **41 ms** | 7.4× |
| two-point density invert | 23.5 ms | **15 ms** | 1.6× |
| reference normalize + look | 113 ms | **37 ms** | 3.1× |

A 24-frame roll (density convert + adjustments) exports to JPEG at ≈32 frames/s
end-to-end.

## Architecture

```
cmd/freeccr        CLI: convert / batch / decode
cmd/freeccrd       local web server (thin wrapper over internal/ui)
cmd/freeccr-app    native desktop app (Wails webview + menu + native dialogs)
internal/ui        embedded SPA + JSON/binary API as one shared http.Handler
internal/convert   negative→positive kernels (bwpoint, reference, look, window)
internal/adjust    fused adjust_image chain + curves/gamma/bands/cineon + LUTs
internal/auto      auto-WB (4 algorithms), auto-exposure, WB eyedropper
internal/geometry  90° rotate, flips, fine straighten, crop
internal/decode    TIFF/JPEG/PNG + RAW via cgo/libraw (-tags libraw); resize
internal/export    16-bit TIFF, 8-bit JPEG, linear DNG
internal/pipeline  batch decode→process→encode workers; shared roll Spec
internal/image     flat float32 interleaved-RGB buffer + sync.Pool
```

## License

FreeCCR-go is a derivative work of FreeCCR and is licensed under **AGPL-3.0** — see
[`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
