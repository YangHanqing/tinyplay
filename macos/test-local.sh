#!/usr/bin/env bash
# Fast local test loop: build, ad-hoc sign, install to /Applications, launch.
#
# This is NOT the notarized release path (release-local.sh) — it skips
# Developer ID signing and notarization entirely. Ad-hoc signing
# (already done by build-app.sh) is enough for an app you build and run on
# your own Mac: Gatekeeper only blocks apps carrying the "downloaded from the
# internet" quarantine flag, which a local build never gets.
#
# ARCH selects the target: arm64 (Apple Silicon, default) or x86_64 (Intel).
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
ARCH="${ARCH:-arm64}"
APP_NAME="TinyPlay.app"
INSTALL_DIR="/Applications"

echo "==> killing any running TinyPlay processes"
pkill -x TinyPlay 2>/dev/null || true
pkill -f "$ROOT/build/tvremote-core" 2>/dev/null || true
pkill -f "$INSTALL_DIR/$APP_NAME" 2>/dev/null || true
sleep 1

echo "==> building $APP_NAME (ARCH=$ARCH)"
cd "$ROOT"
make build-app-mac ARCH="$ARCH"

echo "==> installing to $INSTALL_DIR"
rm -rf "$INSTALL_DIR/$APP_NAME"
ditto "build/$APP_NAME" "$INSTALL_DIR/$APP_NAME"

echo "==> launching"
open "$INSTALL_DIR/$APP_NAME"

echo "==> done: $INSTALL_DIR/$APP_NAME"
