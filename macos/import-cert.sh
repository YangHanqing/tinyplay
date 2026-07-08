#!/usr/bin/env bash
# Imports a Developer ID Application .p12 into a dedicated keychain that stays
# available for the rest of the CI job, so several signing steps in a row (the
# .app, then the .dmg) can all use the identity.
#
# This is deliberately separate from sign-notarize.sh's own self-contained
# import path: that one creates AND deletes a throwaway keychain within a single
# script run, which is wrong when a later, separate step (make-dmg.sh) still
# needs the identity. Here there is no cleanup — the ephemeral CI runner is
# discarded with the VM, so the keychain never outlives the job.
#
# Required env:
#   MACOS_CERTIFICATE       base64 of the Developer ID Application .p12
#   MACOS_CERTIFICATE_PWD   the .p12 export password
set -euo pipefail

: "${MACOS_CERTIFICATE:?missing MACOS_CERTIFICATE}"
: "${MACOS_CERTIFICATE_PWD:?missing MACOS_CERTIFICATE_PWD}"

TMP="${RUNNER_TEMP:-${TMPDIR:-/tmp}}"
KEYCHAIN="$TMP/tinyplay-signing.keychain-db"
KPW="$(openssl rand -base64 24)"
CERT_FILE="$TMP/tinyplay-cert.p12"
trap 'rm -f "$CERT_FILE"' EXIT

echo "$MACOS_CERTIFICATE" | base64 --decode > "$CERT_FILE"

# Preserve the keychains already on the user search list (e.g. login) so adding
# ours does not hide them.
ORIGINAL_KEYCHAINS=()
while IFS= read -r item; do
    ORIGINAL_KEYCHAINS+=("$item")
done < <(security list-keychains -d user | sed -e 's/^[[:space:]]*"//' -e 's/"[[:space:]]*$//')

security create-keychain -p "$KPW" "$KEYCHAIN"
security set-keychain-settings -lut 21600 "$KEYCHAIN"   # no lock-on-sleep, 6h idle timeout
security unlock-keychain -p "$KPW" "$KEYCHAIN"
security import "$CERT_FILE" -k "$KEYCHAIN" -P "$MACOS_CERTIFICATE_PWD" -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$KPW" "$KEYCHAIN" >/dev/null
security list-keychains -d user -s "$KEYCHAIN" "${ORIGINAL_KEYCHAINS[@]}"

echo "==> signing identity imported into $KEYCHAIN"
security find-identity -v -p codesigning "$KEYCHAIN"
