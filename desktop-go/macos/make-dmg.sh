#!/usr/bin/env bash
# Packages an already-signed (and ideally notarized+stapled) .app into a
# distributable DMG with a drag-to-Applications layout, then signs, notarizes,
# and staples the DMG itself.
#
# Uses hdiutil (not create-dmg) so it works on a headless CI runner with no
# logged-in Finder/GUI session. The result is a plain but fully functional DMG:
# it mounts showing the app next to an Applications symlink for drag-install.
# A custom background/icon layout can be added later from a local GUI session.
#
# Required env:
#   SIGN_IDENTITY   Developer ID Application identity (to sign the DMG)
# Optional notarization credentials (same as sign-notarize.sh; choose one):
#   NOTARY_PROFILE  notarytool keychain profile, or
#   AC_API_KEY_PATH path to AuthKey_XXXX.p8 (local), or
#   AC_API_KEY      base64 of AuthKey_XXXX.p8 (CI)  + AC_KEY_ID + AC_ISSUER_ID
#
# Usage: make-dmg.sh "/path/to/TinyPlay.app" "/path/to/out/TinyPlay-macos.dmg"
set -euo pipefail

APP="${1:?usage: make-dmg.sh <app> <out.dmg>}"
DMG="${2:?usage: make-dmg.sh <app> <out.dmg>}"
[ -d "$APP" ] || { echo "app bundle not found: $APP" >&2; exit 1; }
: "${SIGN_IDENTITY:?missing SIGN_IDENTITY}"

VOLNAME="TinyPlay"
TMP="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
STAGE="$(mktemp -d "$TMP/dmgstage.XXXXXX")"
API_KEY_FILE=""
cleanup() {
    [ -n "$API_KEY_FILE" ] && rm -f "$API_KEY_FILE"
    rm -rf "$STAGE"
}
trap cleanup EXIT

echo "==> building DMG: $DMG"
rm -f "$DMG"

# Preferred: create-dmg lays out a drag-to-Applications window (app on the left,
# an arrow-less Applications drop-link on the right, sized nicely). It drives
# Finder via AppleScript, which can be flaky on a headless CI runner, so if it
# is missing or fails we fall back to a plain but functional hdiutil DMG.
app_name="$(basename "$APP")"
if command -v create-dmg >/dev/null 2>&1; then
    ONLY_APP="$STAGE/only-app"
    mkdir -p "$ONLY_APP"
    cp -R "$APP" "$ONLY_APP/"
    create-dmg \
        --volname "$VOLNAME" \
        --window-size 540 380 \
        --icon-size 120 \
        --icon "$app_name" 140 190 \
        --app-drop-link 400 190 \
        --no-internet-enable \
        "$DMG" "$ONLY_APP" || true
fi
if [ ! -f "$DMG" ]; then
    echo "==> create-dmg unavailable/failed; using plain hdiutil layout"
    PLAIN="$STAGE/plain"
    mkdir -p "$PLAIN"
    cp -R "$APP" "$PLAIN/"
    ln -s /Applications "$PLAIN/Applications"
    hdiutil create -volname "$VOLNAME" -srcfolder "$PLAIN" -ov -format UDZO "$DMG"
fi

echo "==> signing DMG"
codesign --force --timestamp --sign "$SIGN_IDENTITY" "$DMG"

# ── Notarize + staple the DMG (optional but expected for public releases) ─────
NOTARY_ARGS=()
if [ -n "${NOTARY_PROFILE:-}" ]; then
    NOTARY_ARGS=(--keychain-profile "$NOTARY_PROFILE")
elif [ -n "${AC_API_KEY_PATH:-}" ] || [ -n "${AC_API_KEY:-}" ]; then
    : "${AC_KEY_ID:?missing AC_KEY_ID}"
    : "${AC_ISSUER_ID:?missing AC_ISSUER_ID}"
    if [ -n "${AC_API_KEY_PATH:-}" ]; then
        [ -f "$AC_API_KEY_PATH" ] || { echo "API key not found: $AC_API_KEY_PATH" >&2; exit 1; }
        API_KEY="$AC_API_KEY_PATH"
    else
        API_KEY_FILE="$TMP/ac_key_dmg-$$.p8"
        echo "$AC_API_KEY" | base64 --decode > "$API_KEY_FILE"
        chmod 600 "$API_KEY_FILE"
        API_KEY="$API_KEY_FILE"
    fi
    NOTARY_ARGS=(--key "$API_KEY" --key-id "$AC_KEY_ID" --issuer "$AC_ISSUER_ID")
fi

if [ ${#NOTARY_ARGS[@]} -gt 0 ]; then
    echo "==> notarizing DMG"
    xcrun notarytool submit "$DMG" "${NOTARY_ARGS[@]}" --wait
    xcrun stapler staple "$DMG"
    xcrun stapler validate "$DMG"
    spctl --assess --type open --context context:primary-signature --verbose=4 "$DMG"
    echo "==> DMG signed, notarized & stapled: $DMG"
else
    echo "==> notarization credentials not set; DMG signed but NOT notarized"
fi
