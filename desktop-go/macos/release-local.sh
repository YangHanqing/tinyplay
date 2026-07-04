#!/usr/bin/env bash
# Build, sign, notarize and package a local macOS release.
# Run from any directory; output is desktop-go/tvremote-macos.zip.
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
VERSION="${VERSION:?set VERSION, for example VERSION=0.9.0}"
: "${SIGN_IDENTITY:?set SIGN_IDENTITY to your Developer ID Application identity}"
SIGN_STAGE="$(mktemp -d)"
trap 'rm -rf "$SIGN_STAGE"' EXIT

command -v brew >/dev/null || { echo "Homebrew is required" >&2; exit 1; }
command -v dylibbundler >/dev/null || { echo "Install dylibbundler: brew install dylibbundler" >&2; exit 1; }
command -v mpv >/dev/null || { echo "Install mpv: brew install mpv" >&2; exit 1; }

cd "$ROOT"
make sync
GOOS=darwin GOARCH=arm64 go build -o build/tvremote-core-darwin-arm64 ./cmd/tvremote

rm -rf mpvstage
mkdir -p mpvstage/bin/libs
REAL_MPV="$(readlink -f "$(command -v mpv)")"
cp "$REAL_MPV" mpvstage/bin/mpv
chmod u+w mpvstage/bin/mpv
dylibbundler -cd -of -od -b -x mpvstage/bin/mpv \
    -d mpvstage/bin/libs -p @executable_path/libs/

VERSION="$VERSION" MPV_DIR="$ROOT/mpvstage" "$HERE/build-app.sh"

# Workspaces under Documents may be managed by a File Provider that attaches a
# protected com.apple.provenance xattr. Sign in a private temporary directory,
# where those attributes can be removed reliably, then package from there.
ditto "$ROOT/build/TV Remote MPV.app" "$SIGN_STAGE/TV Remote MPV.app"
xattr -cr "$SIGN_STAGE/TV Remote MPV.app"
"$HERE/sign-notarize.sh" "$SIGN_STAGE/TV Remote MPV.app"

rm -f "$ROOT/tvremote-macos.zip"
ditto -c -k --keepParent "$SIGN_STAGE/TV Remote MPV.app" "$ROOT/tvremote-macos.zip"

echo "==> release ready: $ROOT/tvremote-macos.zip"
