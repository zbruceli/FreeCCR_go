## FreeCCR-go v0.1.3

A fast, cross-platform Go port of [FreeCCR](https://github.com/toonoumi/FreeCCR):
turn color-negative film scans into positives with a physics-based conversion and
a full color-correction suite — as a native desktop app, a local web UI, or a CLI.

### New in v0.1.3

- Clearer, per-platform desktop **install instructions** (below and in the README).

### Install the desktop app

**macOS (Apple Silicon)**
1. Download **`FreeCCR-go-macos.zip`** and unzip it.
2. Drag **`FreeCCR-go.app`** to Applications and open it. It's Developer ID signed
   and notarized, so it opens with no Gatekeeper warning. libraw and its
   dependencies are bundled — nothing else to install.

**Windows (x64)**
1. Download **`FreeCCR-go-windows.zip`** and unzip it — keep `FreeCCR-go.exe` and
   the `.dll` files together in the same folder.
2. Run **`FreeCCR-go.exe`**. All native libraries are bundled; it uses the
   built-in WebView2 runtime (present on Windows 10/11).

**Linux (x64)**
1. Install the runtime libraries (Debian/Ubuntu; package names vary by distro):
   ```
   sudo apt install libraw23 libgtk-3-0 libwebkit2gtk-4.1-0
   ```
2. Download **`FreeCCR-go-linux`**, make it executable, and run it:
   ```
   chmod +x FreeCCR-go-linux && ./FreeCCR-go-linux
   ```

### Features

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
| macOS (Apple Silicon) | `FreeCCR-go-macos.zip` (`.app`) | none — self-contained, notarized |
| Windows x64 | `FreeCCR-go-windows.zip` | none — self-contained (DLLs bundled) |
| Linux x64 | `FreeCCR-go-linux` | `libraw`, `libgtk-3`, `webkit2gtk-4.1` |

The macOS `.app` (libraw + lcms2/libjpeg-turbo/libomp in `Contents/Frameworks`)
and the Windows `.zip` (libraw + the MinGW runtime DLLs next to the `.exe`) are
**self-contained** — no `brew`/MSYS2 install needed. The Linux binary links its
platform libraries dynamically, so install the three packages above first.

### Build from source

```bash
brew install libraw && make run-app   # macOS desktop app
make serve                            # local web UI at http://127.0.0.1:8422
make build-raw                        # CLI: bin/freeccr
```

Licensed under **AGPL-3.0** (a derivative of FreeCCR).
