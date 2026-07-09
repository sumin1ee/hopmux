#!/usr/bin/env bash
#
# Build hopmux.app and wrap it in a distributable hopmux.dmg (drag-to-Applications).
# Uses `create-dmg` if installed (nicer layout), else falls back to plain hdiutil.
#
#   ./desktop/package-dmg.sh
#
# Output: desktop/hopmux-desktop/build/bin/hopmux.dmg
#
# Note: the .app/.dmg are NOT notarized (no Apple Developer cert). On first run
# the user right-clicks the app -> Open, or runs: xattr -dr com.apple.quarantine hopmux.app
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DESK="$ROOT/desktop/hopmux-desktop"
export PATH="$(go env GOPATH)/bin:$PATH"

echo "==> building hopmux.app (icon + frontend)"
cp "$ROOT/packaging/macos/icon.png" "$DESK/build/appicon.png"
( cd "$DESK" && wails build )

APP="$DESK/build/bin/hopmux.app"
DMG="$DESK/build/bin/hopmux.dmg"
rm -f "$DMG"

if command -v create-dmg >/dev/null 2>&1; then
  echo "==> packaging with create-dmg"
  create-dmg \
    --volname "hopmux" \
    --window-size 540 360 \
    --icon-size 110 \
    --icon "hopmux.app" 150 180 \
    --app-drop-link 390 180 \
    "$DMG" "$APP" >/dev/null
else
  echo "==> packaging with hdiutil (install 'brew install create-dmg' for a nicer layout)"
  STAGE="$(mktemp -d)"
  cp -R "$APP" "$STAGE/"
  ln -s /Applications "$STAGE/Applications"
  hdiutil create -volname "hopmux" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null
  rm -rf "$STAGE"
fi

echo "==> done: $DMG  ($(du -h "$DMG" | cut -f1))"
