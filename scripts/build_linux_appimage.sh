#!/usr/bin/env bash
# Build a self-contained Linux AppImage of the FreeCCR-go desktop app.
#
# Uses linuxdeploy + the GTK plugin to bundle libraw and the GTK/WebKit runtime
# so the app runs on a modern x86_64 desktop distro without apt-installing
# libraw / gtk3 / webkit2gtk. WebKitGTK spawns helper processes (WebKitWeb/
# NetworkProcess) from a libexec dir that ldd can't discover, so we copy that
# dir in and point WebKit at the bundled copies via an AppRun hook.
#
# Intended for the ubuntu CI runner with the build + packaging deps installed:
#   libraw-dev libgtk-3-dev libwebkit2gtk-4.1-dev
#   patchelf file imagemagick librsvg2-dev desktop-file-utils
set -eo pipefail
cd "$(dirname "$0")/.."

ARCH="${ARCH:-x86_64}"
APPDIR="build/AppDir"
TOOLS="build/appimage-tools"
OUT="FreeCCR-go-${ARCH}.AppImage"
MULTIARCH="$(gcc -dumpmachine)"   # e.g. x86_64-linux-gnu

echo "==> compiling (desktop,production,libraw,webkit2_41)..."
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" \
         "$APPDIR/usr/share/icons/hicolor/256x256/apps" "$APPDIR/apprun-hooks"
CGO_ENABLED=1 go build -tags "desktop,production,libraw,webkit2_41" \
  -o "$APPDIR/usr/bin/FreeCCR-go" ./cmd/freeccr-app

echo "==> icon + desktop entry..."
go run ./tools/genicon > /tmp/icon_1024.png
ICON="$APPDIR/usr/share/icons/hicolor/256x256/apps/freeccr-go.png"
if command -v convert >/dev/null; then
  convert /tmp/icon_1024.png -resize 256x256 "$ICON"
else
  cp /tmp/icon_1024.png "$ICON"
fi
cp "$ICON" "$APPDIR/freeccr-go.png"

cat > "$APPDIR/usr/share/applications/freeccr-go.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=FreeCCR-go
Comment=Color-negative film-scan to positive converter
Exec=FreeCCR-go
Icon=freeccr-go
Categories=Graphics;Photography;
Terminal=false
DESKTOP

echo "==> fetching linuxdeploy toolchain..."
mkdir -p "$TOOLS"
dl() { # url dest
  [ -f "$TOOLS/$2" ] || curl -fsSL "$1" -o "$TOOLS/$2"
  chmod +x "$TOOLS/$2"
}
dl https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-x86_64.AppImage linuxdeploy
dl https://raw.githubusercontent.com/linuxdeploy/linuxdeploy-plugin-gtk/master/linuxdeploy-plugin-gtk.sh linuxdeploy-plugin-gtk.sh
dl https://github.com/AppImage/AppImageKit/releases/download/continuous/appimagetool-x86_64.AppImage appimagetool
export PATH="$PWD/$TOOLS:$PATH"
export APPIMAGE_EXTRACT_AND_RUN=1   # CI runners have no FUSE
export ARCH

echo "==> bundling dependencies (libraw + GTK/WebKit) via linuxdeploy..."
linuxdeploy --appdir "$APPDIR" \
  --executable "$APPDIR/usr/bin/FreeCCR-go" \
  --desktop-file "$APPDIR/usr/share/applications/freeccr-go.desktop" \
  --icon-file "$APPDIR/freeccr-go.png" \
  --plugin gtk

echo "==> bundling WebKit helper processes..."
WK_SRC="/usr/lib/${MULTIARCH}/webkit2gtk-4.1"
if [ -d "$WK_SRC" ]; then
  WK_DST="$APPDIR/usr/lib/${MULTIARCH}/webkit2gtk-4.1"
  mkdir -p "$WK_DST"
  cp -a "$WK_SRC/." "$WK_DST/"
  echo "    copied $(ls "$WK_DST" | tr '\n' ' ')"
  cat > "$APPDIR/apprun-hooks/webkit.sh" <<'HOOK'
# Point WebKitGTK at the bundled helper processes / injected bundle so the
# webview works without a system webkit2gtk install.
WK="${APPDIR}/usr/lib/MULTIARCH_PLACEHOLDER/webkit2gtk-4.1"
[ -d "$WK" ] && export WEBKIT_EXEC_PATH="$WK"
[ -d "$WK/injected-bundle" ] && export WEBKIT_INJECTED_BUNDLE_PATH="$WK/injected-bundle"
export WEBKIT_DISABLE_COMPOSITING_MODE=1
HOOK
  sed -i "s/MULTIARCH_PLACEHOLDER/${MULTIARCH}/" "$APPDIR/apprun-hooks/webkit.sh"
else
  echo "    WARNING: $WK_SRC not found; webview may need a system webkit2gtk" >&2
fi

echo "==> packaging $OUT..."
rm -f "$OUT"
appimagetool "$APPDIR" "$OUT"

echo "==> done: $OUT  ($(du -h "$OUT" | cut -f1))"
