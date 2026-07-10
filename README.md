# TinyPlay

> Turn the mini PC you already run into a living-room player, controlled from your phone.

[中文说明](README.zh-CN.md) · **[View the TinyPlay website](https://yanghanqing.github.io/tinyplay/)**

TinyPlay is built for the N100, NUC, mini PC, or Mac that is
already running downloads, Docker containers, or home services. Connect its
often-unused HDMI port to the TV and TinyPlay gives it a second job: mpv-based
media playback with a browser remote on your phone.

The desktop app connects to Emby, Jellyfin, or Plex—or browses SMB/WebDAV
shares directly—then drives the bundled mpv player and serves a phone-friendly
library and remote over your local network. No phone app is required.

TinyPlay also includes a DLNA receiver, enabled by default. A compatible app
on the same trusted LAN can cast its stream directly to TinyPlay; the phone
remote remains available for play, pause, and seek.

## Download

Download the latest build from [GitHub Releases](../../releases/latest).

- **Windows x86-64:** unzip the package, then run `TinyPlay.exe`. Windows may
  show a SmartScreen warning because the current Windows build is unsigned.
- **macOS:** Apple Silicon (`TinyPlay-macos-arm64.dmg`) and Intel
  (`TinyPlay-macos-intel.dmg`) are both available. Open the DMG and drag
  TinyPlay to Applications.

Your phone and computer must be on the same trusted local network. TinyPlay's
remote page has no separate authentication; do not expose its port to the
public internet.

For screenshots, features, setup, and the living-room player buying guide,
visit the **[TinyPlay introduction page](https://yanghanqing.github.io/tinyplay/)**.

## License

TinyPlay's own source code is released under the [MIT License](LICENSE).
Bundled third-party components are distributed under their own licenses; see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
