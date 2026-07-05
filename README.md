# TinyPlay

> Control mpv on your desktop from your phone.

[中文说明](README.zh-CN.md)

## Why TinyPlay

A Mac mini or a Windows NUC tucked under the TV makes a great home media
player: it plays literally any file format, runs whatever apps you want, and
a small fanless box costs far less than the alternatives — while an Apple TV
locks you out of a lot of formats and apps, and a dedicated Blu-ray player
does one thing and nothing else.

The catch is control: picking a movie and adjusting playback speed with a
mouse and keyboard from the couch is annoying. That's the whole reason
TinyPlay exists — it turns your phone into the remote, so the computer just
plays and you never touch a mouse.

An Apple TV native version is coming soon — stay tuned.

Under the hood, playback runs on **mpv**, which plays almost anything you
throw at it — MKV, HEVC, DTS, TrueHD, you name it — with no extra codec
packs to install.

## Download

Grab the latest release for your platform from the [Releases page](../../releases/latest).

## Platforms

- **Windows** (x86-64) — system tray app using the system WebView2 runtime.
- **macOS** — **Apple Silicon (M-series) only**; native AppKit menu-bar app.
  Intel Macs are not supported.

## Features

- **Browse & play** — scroll your Emby library, resume "recently watched"
  items, and search across your whole collection, right from the phone's
  browser.
- **Full playback control** — play/pause, seek, volume, and speed, all from
  the phone.
- **Audio & subtitles** — switch audio and subtitle tracks, nudge subtitle
  delay, and adjust aspect ratio/zoom, without touching the computer.
- **System volume control** — control the desktop's system output volume
  directly from the phone, not just mpv's internal volume.
- **Zero setup on the phone** — the desktop app shows a QR code; scan it and
  you're connected. No install, no account.
- **Runs quietly** — lives in the system tray / menu bar and stays out of the
  way until you start playback from your phone.

## How it fits together

A small Go backend runs on the desktop, talking to your Emby server and
driving mpv over its JSON IPC socket; it serves the same web-based remote UI
to your phone's browser. Your phone is just the remote — all playback
happens locally on the computer.

## License

TinyPlay's own source code is released under the [MIT License](LICENSE).
Bundled third-party components, including mpv, are distributed under their
own licenses — see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
