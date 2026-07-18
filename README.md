# TinyPlay

> Turn an idle Windows mini PC, laptop, or Mac mini into a living-room media player — controlled from your phone.

[中文说明](README.zh-CN.md) · **[View the TinyPlay website](https://yanghanqing.github.io/tinyplay/)**

TinyPlay is designed for idle Windows mini PCs, laptops, and Mac minis. Connect one to
your television and it becomes a living-room media player driven by mpv, with your phone
serving as the remote control.

These machines vastly outperform set-top boxes, yet a keyboard and mouse feel awkward
in front of a TV and a traditional remote is no good at typing searches or scrubbing
through a timeline.

TinyPlay lets each device do what it does best: **let hardware handle playback, let the
phone handle interaction.**

Common hardware already has more than enough video-decode capability:

- Intel 8th-gen iGPUs (UHD 630) hardware-decode 4K HEVC and VP9 10-bit; AV1 support
  arrives on 11th-gen.
- Apple M-series chips hardware-decode 4K HEVC 10-bit across the board, with AV1 on M3.
- A dedicated playback engine generally handles HDR and Dolby Vision more reliably than
  a TV's built-in player, though actual results depend on the source material, system
  settings, and display device.

## Features

- **Phone browser remote** — scan the QR code and you're ready; no app to install
- **Media servers** — connect to Emby, Jellyfin, or Plex for poster walls, episode browsing, search, and resume playback
- **File browsing** — navigate SMB, WebDAV shares, or local directories and play files directly
- **Live TV** — integrate IPTV channel lists with favorites and recent-view history
- **DLNA casting** — cast streams from compatible apps on your local network; the phone remote remains available for playback control
- **Multi-server** — mount multiple media sources simultaneously and switch between them at will
- **Cross-platform** — available for Windows and macOS, including Apple Silicon and Intel
- **mpv under the hood** — MKV, HDR, Dolby Vision, TrueHD, PGS subtitles and more

## Download

Download the latest build from [GitHub Releases](../../releases/latest).

- **Windows x86-64** — unzip the package, then run `TinyPlay.exe`. Windows may
  show a SmartScreen warning because the current build is unsigned.
- **macOS** — Apple Silicon (`TinyPlay-macos-arm64.dmg`) and Intel
  (`TinyPlay-macos-intel.dmg`) are both available. Open the DMG and drag
  TinyPlay to Applications.

The phone and the computer running TinyPlay must be on the same local network.

## Getting Started

For screenshots, feature walkthroughs, and the living-room player buying guide,
visit the **[TinyPlay introduction page](https://yanghanqing.github.io/tinyplay/)**.

## License

TinyPlay is released under the [GNU General Public License v3.0](LICENSE).
Bundled third-party components are distributed under their own licenses; see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
