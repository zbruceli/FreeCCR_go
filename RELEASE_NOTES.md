## FreeCCR-go v0.1.0 — first release

A fast, cross-platform Go port of [FreeCCR](https://github.com/toonoumi/FreeCCR):
turn color-negative film scans into positives with a physics-based conversion and
a full color-correction suite — as a native desktop app, a local web UI, or a CLI.

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
| macOS (Apple Silicon) | `FreeCCR-go-macos.zip` (`.app`) | `brew install libraw` |
| Windows x64 | `FreeCCR-go-windows.exe` | MSYS2/MinGW **libraw** DLLs on `PATH` |
| Linux x64 | `FreeCCR-go-linux` | `libraw`, `libgtk-3`, `webkit2gtk-4.1` |

These are early builds that **link libraw dynamically** rather than bundling it —
self-contained installers (bundled libs, AppImage, notarized `.dmg`, Windows
installer) are planned. Building locally with RAW support just needs the platform
libraw (`brew`/`apt`/MSYS2).

The macOS build here is **ad-hoc signed** (not notarized), so Gatekeeper flags it
on first launch — right-click → **Open**. A local `make app` signs it with your
Developer ID.

### Build from source

```bash
brew install libraw && make run-app   # macOS desktop app
make serve                            # local web UI at http://127.0.0.1:8422
make build-raw                        # CLI: bin/freeccr
```

Licensed under **AGPL-3.0** (a derivative of FreeCCR).
