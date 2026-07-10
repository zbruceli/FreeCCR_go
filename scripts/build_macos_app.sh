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

# --- bundle non-system dylibs (libraw + its deps) into Contents/Frameworks ---
# Rewrites Mach-O load paths to @executable_path/../Frameworks so the app runs
# on a clean Mac with no homebrew install. libraw stays a separate dynamic lib
# (LGPL-2.1 §6 relink freedom). Must run BEFORE signing — install_name_tool
# invalidates code signatures.
echo "==> bundling dylibs into $APP/Contents/Frameworks..."
FRAMEWORKS="$APP/Contents/Frameworks"
mkdir -p "$FRAMEWORKS"

bundle_deps() { # $1 = Mach-O file to scan + rewrite
  local target="$1" dep base
  # non-system deps only (homebrew / /usr/local); the label and any already
  # rewritten @executable_path id won't match, so no self-recursion.
  for dep in $(otool -L "$target" | awk '$1 ~ /^\/opt\/homebrew/ || $1 ~ /^\/usr\/local/ {print $1}'); do
    base="$(basename "$dep")"
    install_name_tool -change "$dep" "@executable_path/../Frameworks/$base" "$target"
    if [ ! -f "$FRAMEWORKS/$base" ]; then
      cp -L "$dep" "$FRAMEWORKS/$base"
      chmod u+w "$FRAMEWORKS/$base"
      install_name_tool -id "@executable_path/../Frameworks/$base" "$FRAMEWORKS/$base"
      bundle_deps "$FRAMEWORKS/$base" # recurse into the copy
    fi
  done
}
bundle_deps "$BIN"

# fail loudly if any homebrew/usr-local path survives anywhere in the bundle
if otool -L "$BIN" "$FRAMEWORKS"/*.dylib 2>/dev/null | grep -q '/opt/homebrew\|/usr/local'; then
  echo "ERROR: unbundled dependency remains:" >&2
  otool -L "$BIN" "$FRAMEWORKS"/*.dylib | grep '/opt/homebrew\|/usr/local' >&2
  exit 1
fi
echo "    bundled: $(cd "$FRAMEWORKS" && echo *.dylib)"

# third-party license texts (LGPL / MIT / BSD / Apache attribution)
LIC="$APP/Contents/Resources/licenses"
mkdir -p "$LIC"
cp -L /opt/homebrew/opt/libraw/LICENSE.LGPL    "$LIC/libraw-LICENSE.LGPL"   2>/dev/null || true
cp -L /opt/homebrew/opt/libraw/LICENSE.CDDL    "$LIC/libraw-LICENSE.CDDL"   2>/dev/null || true
cp -L /opt/homebrew/opt/libraw/COPYRIGHT       "$LIC/libraw-COPYRIGHT"      2>/dev/null || true
cp -L /opt/homebrew/opt/little-cms2/LICENSE    "$LIC/lcms2-LICENSE"         2>/dev/null || true
cp -L /opt/homebrew/opt/jpeg-turbo/LICENSE.md  "$LIC/jpeg-turbo-LICENSE.md" 2>/dev/null || true
cp -L /opt/homebrew/opt/libomp/LICENSE.TXT     "$LIC/libomp-LICENSE.TXT"    2>/dev/null || true

# --- code signing (inside-out: nested dylibs first, then the bundle) ---
if [ -z "${SIGN_IDENTITY:-}" ]; then
  # `|| true`: grep exits 1 (with pipefail) when no Developer ID cert is present
  # (e.g. in CI); fall back to ad-hoc signing instead of aborting.
  SIGN_IDENTITY=$(security find-identity -v -p codesigning 2>/dev/null | grep "Developer ID Application" | head -1 | sed -E 's/.*"(.*)".*/\1/' || true)
  [ -z "$SIGN_IDENTITY" ] && SIGN_IDENTITY="-"
fi
if [ "$SIGN_IDENTITY" = "-" ]; then
  echo "==> ad-hoc signing (no Developer ID identity found; not notarizable)..."
  for d in "$FRAMEWORKS"/*.dylib; do codesign --force --sign - "$d"; done
  codesign --force --sign - "$APP"
else
  echo "==> signing with: $SIGN_IDENTITY (hardened runtime)..."
  for d in "$FRAMEWORKS"/*.dylib; do
    codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" "$d"
  done
  codesign --force --options runtime --timestamp \
    --entitlements scripts/entitlements.plist \
    --sign "$SIGN_IDENTITY" "$APP"
fi
codesign --verify --strict --verbose=2 "$APP" 2>&1 | tail -2

# --- optional notarization ---
# Two credential sources: a stored notarytool keychain profile (NOTARY_PROFILE,
# convenient locally) or direct Apple ID app-specific password (APPLE_ID +
# APPLE_TEAM_ID + APPLE_APP_PWD, convenient in CI). Requires a real Developer ID
# signature; ad-hoc builds can't be notarized. Staples the ticket into the .app
# so it validates offline — do this before zipping the bundle for distribution.
if [ "$SIGN_IDENTITY" != "-" ]; then
  ZIP=/tmp/FreeCCR-go-notarize.zip
  if [ -n "${NOTARY_PROFILE:-}" ]; then
    echo "==> notarizing via keychain profile '$NOTARY_PROFILE'..."
    ditto -c -k --keepParent "$APP" "$ZIP"
    xcrun notarytool submit "$ZIP" --keychain-profile "$NOTARY_PROFILE" --wait
    xcrun stapler staple "$APP"
  elif [ -n "${APPLE_ID:-}" ] && [ -n "${APPLE_TEAM_ID:-}" ] && [ -n "${APPLE_APP_PWD:-}" ]; then
    echo "==> notarizing via Apple ID '$APPLE_ID' (team $APPLE_TEAM_ID)..."
    ditto -c -k --keepParent "$APP" "$ZIP"
    xcrun notarytool submit "$ZIP" \
      --apple-id "$APPLE_ID" --team-id "$APPLE_TEAM_ID" --password "$APPLE_APP_PWD" --wait
    xcrun stapler staple "$APP"
    xcrun stapler validate "$APP"
  else
    echo "==> skipping notarization (set NOTARY_PROFILE, or APPLE_ID + APPLE_TEAM_ID + APPLE_APP_PWD)"
  fi
fi

echo "==> done: $APP  (run: open $APP)"
