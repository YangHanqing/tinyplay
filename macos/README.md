# Native macOS Shell

The macOS build uses AppKit, `NSStatusItem`, and `WKWebView`; it does not use
Electron. The Swift shell owns the menu bar app and QR window, while the Go core
runs as a child process and exposes the local HTTP API. mpv remains a separate
player process.

## App Layout

```text
TinyPlay.app/
  Contents/
    MacOS/TinyPlay
    Resources/
      tvremote-core
      THIRD_PARTY_NOTICES.md
      mpv/bin/mpv
      mpv/bin/libs/*.dylib
```

## Local Development Build

```sh
cd desktop-go
make build-app-mac
# or, for an Intel build cross-compiled from Apple Silicon hardware:
make build-app-mac ARCH=x86_64
```

This creates `build/TinyPlay.app` for local testing. It does not require an
Apple Developer account. `build-app.sh` compiles the Swift shell, assembles the
bundle, and applies an ad-hoc signature so the app can run locally.

`ARCH` (`arm64`, the default, or `x86_64`) selects the target architecture; it
is forwarded from `make build-app-mac` to both `build-core-mac` (Go core) and
`build-app.sh` (Swift shell), which cross-compiles via `swiftc -target
$ARCH-apple-macosx13.0`. Both architectures share the same `LSMinimumSystemVersion`
(13.0), since that floor comes from a Swift API used by the shell, not from the
architecture.

If `MPV_DIR` is not set, the app falls back to `mpv` on `PATH`. Release builds
normally set `MPV_DIR` to a self-contained mpv directory so the app can bundle
mpv under `Contents/Resources/mpv`.

An ad-hoc signed local build is not notarized. macOS Gatekeeper may warn that it
cannot verify the developer, especially after the app is downloaded or copied in
a way that adds quarantine metadata. That is expected for non-release builds.

## Local Release Build

Official local releases require a Developer ID Application certificate and
notarization credentials. The App Store Connect API private key must stay
outside every Git repository and should be readable only by the current user
(`chmod 600`).

```sh
VERSION=0.9.0 \
SIGN_IDENTITY="Developer ID Application: ... (TEAMID)" \
AC_API_KEY_PATH="/secure/path/AuthKey_XXXX.p8" \
AC_KEY_ID=XXXX \
AC_ISSUER_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx \
./macos/release-local.sh
```

`release-local.sh` performs the release pipeline:

1. Builds the Go core and Swift shell.
2. Stages mpv and rewrites its dynamic library references.
3. Signs nested code with the Developer ID identity.
4. Submits the app to Apple notarization with `notarytool`.
5. Staples the notarization ticket and verifies the app with Gatekeeper.
6. Produces `TinyPlay-macos-arm64.dmg`.

Set `ARCH=x86_64` to build the Intel variant instead (`TinyPlay-macos-intel.dmg`).
mpv is staged from whatever `mpv`/`dylibbundler` resolve to on `PATH`, so
building the Intel variant on Apple Silicon hardware requires an x86_64
Homebrew prefix (e.g. a Rosetta-installed `/usr/local` Homebrew) ahead of the
native one on `PATH`.

If the project is stored under a macOS File Provider managed location such as
Documents, build artifacts may receive protected `com.apple.provenance`
extended attributes. The release script signs from a private temporary directory
so those attributes can be removed reliably before sealing the app.
