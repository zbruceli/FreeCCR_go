#!/usr/bin/env bash
# Build FreeCCR-go.app — the native desktop build (Wails webview + libraw RAW),
# with a generated icon and code signing. Produces a double-clickable bundle at
# bin/FreeCCR-go.app.
#
# Requirements: Go, Xcode command line tools (WebKit), libraw (brew install libraw).
# Signing: auto-uses a "Developer ID Application" identity if one is in the
#   keychain; otherwise ad-hoc signs. Override with SIGN_IDENTITY="...".
# Notarize (optional): set NOTARY_PROFILE to a stored `notarytool` profile name.
set -eo pipefail
cd "$(dirname "$0")/.."

APP="bin/FreeCCR-go.app"
BIN="$APP/Contents/MacOS/FreeCCR"
VERSION="${VERSION:-0.1.0}"
ICONSET=/tmp/FreeCCR.iconset

echo "==> compiling (desktop,production,libraw)..."
CGO_ENABLED=1 CGO_LDFLAGS="-framework UniformTypeIdentifiers" CGO_LDFLAGS_ALLOW="-Xpreprocessor|-fopenmp" go build -tags "desktop,production,libraw" -o /tmp/freeccr-app-bin ./cmd/freeccr-app

echo "==> generating icon..."
go run ./tools/genicon > /tmp/icon_1024.png
rm -rf "$ICONSET"; mkdir -p "$ICONSET"
for sz in 16 32 128 256 512; do
  sips -z "$sz" "$sz" /tmp/icon_1024.png --out "$ICONSET/icon_${sz}x${sz}.png" >/dev/null
  d=$((sz * 2))
  sips -z "$d" "$d" /tmp/icon_1024.png --out "$ICONSET/icon_${sz}x${sz}@2x.png" >/dev/null
done

echo "==> assembling $APP..."
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
mv /tmp/freeccr-app-bin "$BIN"; chmod +x "$BIN"
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icon.icns"

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
  <key>CFBundleIconFile</key><string>icon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>10.15</string>
  <key>NSHighResolutionCapable</key><true/>
</dict></plist>
PLIST

# --- code signing ---
if [ -z "${SIGN_IDENTITY:-}" ]; then
  SIGN_IDENTITY=$(security find-identity -v -p codesigning 2>/dev/null | grep "Developer ID Application" | head -1 | sed -E 's/.*"(.*)".*/\1/')
  [ -z "$SIGN_IDENTITY" ] && SIGN_IDENTITY="-"
fi
if [ "$SIGN_IDENTITY" = "-" ]; then
  echo "==> ad-hoc signing (no Developer ID identity found; not notarizable)..."
  codesign --force --deep --sign - "$APP"
else
  echo "==> signing with: $SIGN_IDENTITY (hardened runtime)..."
  codesign --force --deep --options runtime --timestamp \
    --entitlements scripts/entitlements.plist \
    --sign "$SIGN_IDENTITY" "$APP"
fi
codesign --verify --strict --verbose=2 "$APP" 2>&1 | tail -2

# --- optional notarization ---
if [ -n "${NOTARY_PROFILE:-}" ] && [ "$SIGN_IDENTITY" != "-" ]; then
  echo "==> notarizing via profile '$NOTARY_PROFILE'..."
  ditto -c -k --keepParent "$APP" /tmp/FreeCCR-go.zip
  xcrun notarytool submit /tmp/FreeCCR-go.zip --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "$APP"
fi

echo "==> done: $APP  (run: open $APP)"
