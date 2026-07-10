#!/usr/bin/env bash
# Build FreeCCR-go.app — the native desktop build (Wails webview + libraw RAW).
# Produces bin/FreeCCR-go.app, a double-clickable macOS bundle.
# Requirements: Go, Xcode command line tools (WebKit), libraw (brew install libraw).
set -eo pipefail
cd "$(dirname "$0")/.."

APP="bin/FreeCCR-go.app"
BIN="$APP/Contents/MacOS/FreeCCR"
VERSION="${VERSION:-0.1.0}"

echo "==> compiling (desktop,production,libraw)..."
CGO_ENABLED=1 CGO_LDFLAGS="-framework UniformTypeIdentifiers" CGO_LDFLAGS_ALLOW="-Xpreprocessor|-fopenmp" go build -tags "desktop,production,libraw" -o /tmp/freeccr-app-bin ./cmd/freeccr-app

echo "==> assembling $APP..."
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
mv /tmp/freeccr-app-bin "$BIN"
chmod +x "$BIN"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>FreeCCR-go</string>
  <key>CFBundleDisplayName</key><string>FreeCCR-go</string>
  <key>CFBundleIdentifier</key><string>com.zbruceli.freeccrgo</string>
  <key>CFBundleVersion</key><string>${VERSION}</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>CFBundleExecutable</key><string>FreeCCR</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>10.15</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST

echo "==> done: $APP  (run: open $APP)"
