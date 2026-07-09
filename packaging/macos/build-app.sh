#!/usr/bin/env bash
#
# Build a double-clickable Hopmux.app bundle for macOS.
#
# hopmux is a terminal UI, so the .app can't just run the raw binary (a
# double-clicked .app has no TTY). Instead the bundle's executable is a tiny
# launcher that opens Terminal.app running the real hopmux binary.
#
# Usage:
#   packaging/macos/build-app.sh [arm64|amd64]   (default: host arch)
#
# Output: dist/Hopmux.app
#
# Icon: drop a 1024x1024 PNG at packaging/macos/icon.png (or an .icns at
# packaging/macos/Hopmux.icns) before running; this script converts PNG->icns
# automatically. Without one, the app uses the default icon.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ARCH="${1:-$(uname -m)}"
case "$ARCH" in
  arm64|aarch64) GOARCH=arm64 ;;
  x86_64|amd64)  GOARCH=amd64 ;;
  *) echo "unknown arch: $ARCH"; exit 1 ;;
esac

APP="$ROOT/dist/Hopmux.app"
CONTENTS="$APP/Contents"
MACOS="$CONTENTS/MacOS"
RES="$CONTENTS/Resources"

echo "==> building hopmux binary (darwin/$GOARCH)"
mkdir -p "$MACOS" "$RES"
GOOS=darwin GOARCH="$GOARCH" go build -ldflags "-s -w" \
  -o "$MACOS/hopmux-bin" "$ROOT"

echo "==> writing launcher"
# The bundle's main executable: open Terminal.app running the bundled binary.
cat > "$MACOS/Hopmux" <<'LAUNCHER'
#!/bin/sh
DIR="$(cd "$(dirname "$0")" && pwd)"
BIN="$DIR/hopmux-bin"
# Open Terminal.app and run hopmux inside it. `open -a` gives us a real TTY.
open -a Terminal "$BIN"
LAUNCHER
chmod +x "$MACOS/Hopmux" "$MACOS/hopmux-bin"

echo "==> writing Info.plist"
cat > "$CONTENTS/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>            <string>Hopmux</string>
  <key>CFBundleDisplayName</key>     <string>hopmux</string>
  <key>CFBundleIdentifier</key>      <string>dev.hopmux.app</string>
  <key>CFBundleVersion</key>         <string>0.2.0</string>
  <key>CFBundleShortVersionString</key><string>0.2.0</string>
  <key>CFBundlePackageType</key>     <string>APPL</string>
  <key>CFBundleExecutable</key>      <string>Hopmux</string>
  <key>CFBundleIconFile</key>        <string>Hopmux</string>
  <key>LSMinimumSystemVersion</key>  <string>11.0</string>
  <key>NSHighResolutionCapable</key> <true/>
</dict>
</plist>
PLIST

echo "==> icon"
ICNS="$ROOT/packaging/macos/Hopmux.icns"
PNG="$ROOT/packaging/macos/icon.png"
if [ -f "$ICNS" ]; then
  cp "$ICNS" "$RES/Hopmux.icns"
  echo "    used packaging/macos/Hopmux.icns"
elif [ -f "$PNG" ]; then
  # convert a 1024 PNG into a proper multi-resolution .icns
  TMP="$(mktemp -d)/Hopmux.iconset"; mkdir -p "$TMP"
  for s in 16 32 64 128 256 512; do
    sips -z $s $s     "$PNG" --out "$TMP/icon_${s}x${s}.png"      >/dev/null
    sips -z $((s*2)) $((s*2)) "$PNG" --out "$TMP/icon_${s}x${s}@2x.png" >/dev/null
  done
  iconutil -c icns "$TMP" -o "$RES/Hopmux.icns"
  echo "    generated Hopmux.icns from icon.png"
else
  echo "    (no icon found — drop packaging/macos/icon.png to add one)"
fi

echo "==> done: $APP"
echo "    first launch: right-click the app -> Open (unsigned; Gatekeeper)."