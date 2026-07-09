#!/usr/bin/env bash
#
# Build the self-contained hopmux.app for macOS:
#   1. build the hopmux TUI binary (the engine the window hosts)
#   2. wails build (the window/app shell with icon)
#   3. drop the TUI binary into the app's Resources so the app is self-contained
#
# Output: desktop/hopmux-desktop/build/bin/hopmux.app
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DESK="$ROOT/desktop/hopmux-desktop"
export PATH="$(go env GOPATH)/bin:$PATH"

echo "==> building hopmux TUI engine"
go build -ldflags "-s -w" -o "$DESK/build/hopmux-tui" "$ROOT"

echo "==> wails build (app shell + icon)"
cp "$ROOT/packaging/macos/icon.png" "$DESK/build/appicon.png"
( cd "$DESK" && wails build )

APP="$DESK/build/bin/hopmux.app"
echo "==> bundling TUI engine into $APP/Contents/Resources"
# MUST be named hopmux-tui (NOT hopmux) — the app's own binary is MacOS/hopmux,
# and spawning that would fork-bomb the GUI into infinite windows.
cp "$DESK/build/hopmux-tui" "$APP/Contents/Resources/hopmux-tui"
chmod +x "$APP/Contents/Resources/hopmux-tui"

echo "==> done: $APP"
echo "    run it:  open '$APP'"
