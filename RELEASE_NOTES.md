## FreeCCR-go v0.1.2

A fast, cross-platform Go port of [FreeCCR](https://github.com/toonoumi/FreeCCR):
turn color-negative film scans into positives with a physics-based conversion and
a full color-correction suite — as a native desktop app, a local web UI, or a CLI.

### New in v0.1.2

- **Self-contained Windows build** — libraw and the MinGW runtime DLLs are bundled
  in `FreeCCR-go-windows.zip`; unzip and run, no MSYS2 install needed.

### Also in v0.1.1

- **Self-contained macOS app** — libraw and its dependencies are bundled into the
  `.app`, so it runs with no `brew install`.
- **Signed & notarized** — the macOS build is Developer ID signed and notarized by
  Apple, so it launches with no Gatekeeper warning.

### Highlights

- **Conversion** — two-point B/W-point (linear + optical-density), default-slope, and auto/reference modes, with working-space highlight headroom.
- **Color correction** — WB/temp/tint, exposure, tone, contrast, saturation, per-channel levels, monotone-cubic **curves**, **gamma**, **7-band HSV color vectors**, and Cineon→Rec.709.
- **Analysis & view** — live RGB histogram, RGB parade + vectorscope, hover probe, crop/straighten/rotate/flip, zoom & pan.
- **Assisted** — auto-WB (4 algorithms), auto-exposure, WB eyedropper.
- **I/O** — RAW (CR2/CR3/NEF/ARW/DNG/…) via libraw + TIFF/JPEG/PNG in; 16-bit TIFF, 8-bit JPEG, or linear DNG out.
- **Roll workflow** — batch convert/export, Sync-to-All, copy/paste.
- **Fast & faithful** — several times quicker than the numpy original, validated bit-exact against the real FreeCCR Python.

### Downloads

| Platform | Asset | Runtime needs |
|---|---|---|
| macOS (Apple Silicon) | `FreeCCR-go-macos.zip` (`.app`) | none — libraw & deps are bundled |
| Windows x64 | `FreeCCR-go-windows.zip` | none — unzip and run (DLLs bundled) |
| Linux x64 | `FreeCCR-go-linux` | `libraw`, `libgtk-3`, `webkit2gtk-4.1` |

The macOS `.app` (libraw + lcms2/libjpeg-turbo/libomp in `Contents/Frameworks`)
and the Windows `.zip` (libraw + the MinGW runtime DLLs next to the `.exe`) are
**self-contained** — no `brew`/MSYS2 install needed to run them. The Linux build
still links its platform libraries dynamically; a self-contained AppImage is
planned. Building locally with RAW support just needs the platform libraw
(`brew`/`apt`/MSYS2).

The macOS `.app` is **signed with a Developer ID and notarized by Apple**, so it
launches without a Gatekeeper warning — no right-click → Open needed.

### Build from source

```bash
brew install libraw && make run-app   # macOS desktop app
make serve                            # local web UI at http://127.0.0.1:8422
make build-raw                        # CLI: bin/freeccr
```

Licensed under **AGPL-3.0** (a derivative of FreeCCR).
