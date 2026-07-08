#!/usr/bin/env bash
# Signs the .app with a Developer ID Application certificate (hardened runtime)
# and, if App Store Connect API credentials are present, notarizes + staples it.
#
# Designed to run in CI from secrets, but also works locally. Nothing is printed
# that would leak the certificate or its password.
#
# Required env:
#   SIGN_IDENTITY           e.g. "Developer ID Application: Your Name (TEAMID)"
# Optional CI-only certificate import (local releases use the login keychain):
#   MACOS_CERTIFICATE       base64 of the Developer ID Application .p12
#   MACOS_CERTIFICATE_PWD   the .p12 export password
# Optional notarization credentials (choose one authentication method):
#   NOTARY_PROFILE          notarytool keychain profile (best for local use), or
#   AC_API_KEY_PATH         path to AuthKey_XXXX.p8 (local use), or
#   AC_API_KEY              base64 of AuthKey_XXXX.p8 (CI use)
#   AC_KEY_ID               the key id (e.g. 2X9R4HXF34)
#   AC_ISSUER_ID            the issuer uuid
#
# Usage: sign-notarize.sh "/path/to/TinyPlay.app"
set -euo pipefail

APP="${1:?usage: sign-notarize.sh <app>}"
[ -d "$APP" ] || { echo "app bundle not found: $APP" >&2; exit 1; }
HERE="$(cd "$(dirname "$0")" && pwd)"
ENT="$HERE/entitlements.plist"
MPV_ENT="$HERE/mpv-entitlements.plist"
TMP="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"

: "${SIGN_IDENTITY:?missing SIGN_IDENTITY}"

KEYCHAIN=""
CERT_FILE=""
API_KEY_FILE=""
NOTARY_ZIP=""
ORIGINAL_KEYCHAINS=()

cleanup() {
    [ -n "$CERT_FILE" ] && rm -f "$CERT_FILE"
    [ -n "$API_KEY_FILE" ] && rm -f "$API_KEY_FILE"
    [ -n "$NOTARY_ZIP" ] && rm -f "$NOTARY_ZIP"
    if [ -n "$KEYCHAIN" ]; then
        security list-keychains -d user -s "${ORIGINAL_KEYCHAINS[@]}" >/dev/null 2>&1 || true
        security delete-keychain "$KEYCHAIN" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

# CI imports a .p12 into a throwaway keychain. Local releases deliberately use
# the Developer ID identity already protected by the user's login keychain.
if [ -n "${MACOS_CERTIFICATE:-}" ]; then
    : "${MACOS_CERTIFICATE_PWD:?missing MACOS_CERTIFICATE_PWD}"
    KEYCHAIN="$TMP/app-signing-$$.keychain-db"
    KPW="$(openssl rand -base64 24)"
    while IFS= read -r item; do
        ORIGINAL_KEYCHAINS+=("$item")
    done < <(security list-keychains -d user | sed -e 's/^[[:space:]]*"//' -e 's/"[[:space:]]*$//')
    security create-keychain -p "$KPW" "$KEYCHAIN"
    security set-keychain-settings -lut 21600 "$KEYCHAIN"
    security unlock-keychain -p "$KPW" "$KEYCHAIN"
    CERT_FILE="$TMP/cert-$$.p12"
    echo "$MACOS_CERTIFICATE" | base64 --decode > "$CERT_FILE"
    security import "$CERT_FILE" -k "$KEYCHAIN" -P "$MACOS_CERTIFICATE_PWD" -T /usr/bin/codesign
    security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$KPW" "$KEYCHAIN" >/dev/null
    security list-keychains -d user -s "$KEYCHAIN" "${ORIGINAL_KEYCHAINS[@]}"
elif ! security find-identity -v -p codesigning | grep -Fq "\"$SIGN_IDENTITY\""; then
    echo "Developer ID identity not found in the login keychain: $SIGN_IDENTITY" >&2
    exit 1
fi

# Finder metadata and resource-fork xattrs can make an otherwise valid local
# build unsignable. This bundle is a generated release artifact, so remove them
# before sealing its contents.
xattr -cr "$APP"

# ── Sign inside-out: dylibs, then mpv + core, then the shell + bundle ────────
echo "==> signing nested code"
while IFS= read -r -d '' f; do
    codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" "$f"
done < <(find "$APP/Contents/Resources" -type f -name '*.dylib' -print0)

MPV_BIN="$APP/Contents/Resources/mpv/bin/mpv"
if [ -f "$MPV_BIN" ]; then
    codesign --force --options runtime --timestamp --entitlements "$MPV_ENT" --sign "$SIGN_IDENTITY" "$MPV_BIN"
fi

CORE_BIN="$APP/Contents/Resources/tvremote-core"
if [ -f "$CORE_BIN" ]; then
    codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" "$CORE_BIN"
fi

echo "==> signing main executable + bundle"
codesign --force --options runtime --timestamp --entitlements "$ENT" --sign "$SIGN_IDENTITY" "$APP/Contents/MacOS/TinyPlay"
codesign --force --options runtime --timestamp --entitlements "$ENT" --sign "$SIGN_IDENTITY" "$APP"
codesign --verify --strict --verbose=2 "$APP"

# ── Notarize + staple (optional) ─────────────────────────────────────────────
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
        API_KEY_FILE="$TMP/ac_key-$$.p8"
        echo "$AC_API_KEY" | base64 --decode > "$API_KEY_FILE"
        chmod 600 "$API_KEY_FILE"
        API_KEY="$API_KEY_FILE"
    fi
    NOTARY_ARGS=(--key "$API_KEY" --key-id "$AC_KEY_ID" --issuer "$AC_ISSUER_ID")
fi

if [ ${#NOTARY_ARGS[@]} -gt 0 ]; then
    echo "==> notarizing"
    NOTARY_ZIP="$TMP/notarize-$$.zip"
    ditto -c -k --keepParent "$APP" "$NOTARY_ZIP"
    xcrun notarytool submit "$NOTARY_ZIP" "${NOTARY_ARGS[@]}" --wait
    xcrun stapler staple "$APP"
    rm -f "$NOTARY_ZIP"
    NOTARY_ZIP=""
    codesign --verify --deep --strict --verbose=2 "$APP"
    xcrun stapler validate "$APP"
    spctl --assess --type execute --verbose=4 "$APP"
    echo "==> signed, notarized & stapled"
else
    echo "==> notarization credentials not set; signed but NOT notarized"
fi
