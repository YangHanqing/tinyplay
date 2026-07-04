#!/usr/bin/env bash
# Assembles the native macOS .app:
#   1. compiles macos/Sources/main.swift with swiftc (no .xcodeproj),
#   2. lays out the .app bundle,
#   3. drops in the Go core and (optionally) a bundled mpv,
#   4. ad-hoc signs so it launches locally; real Developer-ID signing +
#      notarization is done separately on the developer's Mac (never in CI).
#
# Inputs (env, all optional):
#   CORE_BIN  path to the built Go core   (default: ../build/tvremote-core-darwin-arm64)
#   MPV_DIR   dir copied to Contents/Resources/mpv; expects bin/mpv inside it
#   OUT       output .app path            (default: ../build/TinyPlay.app)
#   VERSION   CFBundleShortVersionString  (default: 0.1.0)
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
CORE_BIN="${CORE_BIN:-$HERE/../build/tvremote-core-darwin-arm64}"
OUT="${OUT:-$HERE/../build/TinyPlay.app}"
VERSION="${VERSION:-0.1.0}"

[ -f "$CORE_BIN" ] || { echo "core binary not found: $CORE_BIN (run make build-core-mac)"; exit 1; }

echo "==> compiling Swift shell"
mkdir -p "$HERE/../build"
SHELL_BIN="$HERE/../build/TVRemoteShell"
swiftc -O -o "$SHELL_BIN" "$HERE/Sources/main.swift" \
    -framework AppKit -framework WebKit

echo "==> compiling AVPlayer helper (Dolby Vision P5 fallback)"
HELPER_BIN="$HERE/../build/avplayer-helper"
swiftc -O -o "$HELPER_BIN" "$HERE/Sources/avplayer-helper.swift" \
    -framework AppKit -framework AVKit -framework AVFoundation

echo "==> assembling bundle: $OUT"
rm -rf "$OUT"
MACOS="$OUT/Contents/MacOS"
RES="$OUT/Contents/Resources"
mkdir -p "$MACOS" "$RES"

cp "$SHELL_BIN" "$MACOS/TinyPlay"
cp "$CORE_BIN"  "$RES/tvremote-core"
cp "$HELPER_BIN" "$RES/avplayer-helper"
chmod +x "$MACOS/TinyPlay" "$RES/tvremote-core" "$RES/avplayer-helper"

if [ -n "${MPV_DIR:-}" ] && [ -d "$MPV_DIR" ]; then
    echo "==> bundling mpv from $MPV_DIR"
    rm -rf "$RES/mpv"
    cp -R "$MPV_DIR" "$RES/mpv"
    [ -f "$RES/mpv/bin/mpv" ] && chmod +x "$RES/mpv/bin/mpv" || true
else
    echo "==> no MPV_DIR given; the app will fall back to mpv on PATH"
fi

cat > "$OUT/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key><string>TinyPlay</string>
    <key>CFBundleDisplayName</key><string>TinyPlay</string>
    <key>CFBundleIdentifier</key><string>cn.hqyang.tinyplay.mac</string>
    <key>CFBundleExecutable</key><string>TinyPlay</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleShortVersionString</key><string>$VERSION</string>
    <key>CFBundleVersion</key><string>$VERSION</string>
    <key>LSMinimumSystemVersion</key><string>13.0</string>
    <key>LSUIElement</key><true/>
    <key>NSHighResolutionCapable</key><true/>
    <key>NSLocalNetworkUsageDescription</key><string>Needs local network access to connect to your Emby server and control mpv on this computer.</string>
    <key>NSBonjourServices</key><array><string>_http._tcp</string></array>
</dict>
</plist>
PLIST

echo "==> ad-hoc signing (CI artifact is UNSIGNED for distribution; sign locally)"
codesign --force --deep --sign - "$OUT" 2>/dev/null || true

echo "==> done: $OUT"
