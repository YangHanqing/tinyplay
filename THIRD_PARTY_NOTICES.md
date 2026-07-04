# Third-party notices

This file is a pre-release inventory, not yet a complete binary distribution
notice. The final release must record exact versions, source URLs, license texts,
and source-availability obligations for every bundled component.

## Bundled at runtime

- mpv — GPL-2.0-or-later by default, or LGPL-2.1-or-later when built with the
  corresponding GPL-disabled configuration. The exact downloaded build and its
  configuration must be recorded for each release.
- FFmpeg and codec libraries pulled into the selected mpv build — licensing
  depends on the exact build configuration.

## Go source dependencies

- fyne.io/systray
- github.com/Microsoft/go-winio
- github.com/jchv/go-webview2
- github.com/skip2/go-qrcode
- their transitive dependencies recorded in `desktop-go/go.sum`

Do not treat this inventory as a substitute for shipping the required license
texts with binary releases.
