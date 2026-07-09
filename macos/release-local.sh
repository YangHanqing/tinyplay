#!/usr/bin/env bash
# Build, sign, notarize and package a local macOS release.
# Run from any directory; output is desktop-go/TinyPlay-macos-<arch-label>.dmg.
#
# ARCH selects the target: arm64 (Apple Silicon, default) or x86_64 (Intel).
# mpv is staged from whatever `mpv`/`dylibbundler` resolve to on PATH, so on
# Apple Silicon hardware building the x86_64 variant requires an x86_64
# Homebrew prefix (e.g. Rosetta's /usr/local) ahead of the native one on PATH.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
VERSION="${VERSION:?set VERSION, for example VERSION=0.9.0}"
ARCH="${ARCH:-arm64}"
: "${SIGN_IDENTITY:?set SIGN_IDENTITY to your Developer ID Application identity}"
case "$ARCH" in
    arm64) GOARCH=arm64; LABEL=arm64 ;;
    x86_64) GOARCH=amd64; LABEL=intel ;;
    *) echo "unsupported ARCH: $ARCH (expected arm64 or x86_64)" >&2; exit 1 ;;
esac
SIGN_STAGE="$(mktemp -d)"
trap 'rm -rf "$SIGN_STAGE"' EXIT

command -v brew >/dev/null || { echo "Homebrew is required" >&2; exit 1; }
command -v dylibbundler >/dev/null || { echo "Install dylibbundler: brew install dylibbundler" >&2; exit 1; }
command -v mpv >/dev/null || { echo "Install mpv: brew install mpv" >&2; exit 1; }

cd "$ROOT"
GOOS=darwin GOARCH="$GOARCH" go build -o "build/tvremote-core-darwin-$GOARCH" ./cmd/tvremote

rm -rf mpvstage
mkdir -p mpvstage/bin/libs
REAL_MPV="$(readlink -f "$(command -v mpv)")"
cp "$REAL_MPV" mpvstage/bin/mpv
chmod u+w mpvstage/bin/mpv
dylibbundler -cd -of -od -b -x mpvstage/bin/mpv \
    -d mpvstage/bin/libs -p @executable_path/libs/

ARCH="$ARCH" VERSION="$VERSION" MPV_DIR="$ROOT/mpvstage" "$HERE/build-app.sh"

# Workspaces under Documents may be managed by a File Provider that attaches a
# protected com.apple.provenance xattr. Sign in a private temporary directory,
# where those attributes can be removed reliably, then package from there.
ditto "$ROOT/build/TinyPlay.app" "$SIGN_STAGE/TinyPlay.app"
xattr -cr "$SIGN_STAGE/TinyPlay.app"
"$HERE/sign-notarize.sh" "$SIGN_STAGE/TinyPlay.app"

OUT_DMG="$ROOT/TinyPlay-macos-$LABEL.dmg"
rm -f "$OUT_DMG"
"$HERE/make-dmg.sh" "$SIGN_STAGE/TinyPlay.app" "$OUT_DMG"

echo "==> release ready: $OUT_DMG"
