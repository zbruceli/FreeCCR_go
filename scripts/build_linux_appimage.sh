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

echo "==> completeness pass: bundle excluded-but-needed libs..."
# linuxdeploy's excludelist omits libs it assumes the host provides (harfbuzz,
# glib, fontconfig, …), which breaks self-containment. Walk the DT_NEEDED closure
# of everything in the AppDir and copy in any dependency that isn't already
# bundled, except the genuine host-base libs an AppImage must inherit (glibc, X,
# GL, the C++/gcc runtime) — bundling those would clash with the host.
#
# We resolve deps by NAME via the host loader cache rather than with ldd: once a
# lib is bundled, patchelf gives it a bundle-local RUNPATH, so ldd reports its
# still-unbundled transitive deps as "not found" (no path) and they'd be missed.
# Bundle into the flat usr/lib that linuxdeploy's AppRun puts on LD_LIBRARY_PATH.
LIBDIR="$APPDIR/usr/lib"
mkdir -p "$LIBDIR"
HOST_RE='^(ld-linux.*|libc|libm|libdl|librt|libpthread|libresolv|libnsl|libutil|libgcc_s|libstdc\+\+|libGL|libGLX|libGLdispatch|libEGL|libOpenGL|libGLU|libX11|libX11-xcb|libxcb.*|libXext|libXrender|libXrandr|libXi|libXcursor|libXfixes|libXdamage|libXcomposite|libXtst|libXau|libXdmcp|libxkbcommon|libxshmfence|libdrm|libgbm|libwayland.*)\.so'

declare -A HOSTLIB
while IFS= read -r line; do
  n="${line%% *}"; p="${line##*=> }"
  [ -n "$n" ] && [ -n "$p" ] && [ -z "${HOSTLIB[$n]:-}" ] && HOSTLIB[$n]="$p"
done < <(ldconfig -p | sed -nE 's/^[[:space:]]*([^ ]+) .*=> (.*)$/\1 => \2/p')

for _pass in 1 2 3 4 5 6 7 8; do
  added=0
  while IFS= read -r f; do
    for n in $(objdump -p "$f" 2>/dev/null | awk '/NEEDED/ {print $2}'); do
      echo "$n" | grep -qE "$HOST_RE" && continue
      [ -e "$LIBDIR/$n" ] && continue
      src="${HOSTLIB[$n]:-}"
      [ -n "$src" ] && [ -e "$src" ] || src="$(find /usr/lib /lib -name "$n" 2>/dev/null | head -1)"
      [ -n "$src" ] && [ -e "$src" ] || continue
      cp -L "$src" "$LIBDIR/$n" && { added=$((added + 1)); echo "      + $n"; }
    done
  done < <(find "$APPDIR/usr" -type f \( -name '*.so*' -o -perm -u+x \))
  echo "    pass $_pass: +$added libs"
  [ "$added" -eq 0 ] && break
done

echo "==> DEBUG graphite2: needed-by / cache / present..."
for f in $(find "$APPDIR/usr" -type f \( -name '*.so*' -o -perm -u+x \)); do
  objdump -p "$f" 2>/dev/null | awk '/NEEDED/ {print $2}' | grep -q '^libgraphite2' && echo "    NEEDED-by: ${f#$APPDIR/}"
done
echo "    in ldconfig cache: ${HOSTLIB[libgraphite2.so.3]:-<none>}"
echo "    on host: $(find /usr/lib /lib -name 'libgraphite2.so*' 2>/dev/null | head -1 || echo none)"
echo "    bundled: $(ls "$LIBDIR"/libgraphite2.so* 2>/dev/null || echo none)"

echo "==> packaging $OUT..."
rm -f "$OUT"
appimagetool "$APPDIR" "$OUT"

echo "==> done: $OUT  ($(du -h "$OUT" | cut -f1))"
