# Third-party notices

TinyPlay is licensed under the GNU GPL v3.0 (see [LICENSE](LICENSE)).
The components below are used by the desktop app and remain under their own
licenses. Exact Go module versions are pinned in `go.mod` / `go.sum`.

## Bundled player (binary packages)

Release packages may ship **mpv** and libraries it links, commonly including
**FFmpeg** and related codecs:

| Component | Typical license | Upstream |
|---|---|---|
| mpv | GPL-2.0-or-later (some builds LGPL-2.1-or-later) | https://mpv.io |
| FFmpeg and linked codec libraries | LGPL and/or GPL, per build | https://ffmpeg.org |
| Vulkan-Loader (`vulkan-1.dll`, Windows only) | Apache-2.0 | https://github.com/KhronosGroup/Vulkan-Loader |

- **Windows:** CI currently downloads an x86_64 build from
  [zhongfly/mpv-winbuild](https://github.com/zhongfly/mpv-winbuild). It also
  bundles the Khronos Vulkan loader, because mpv.exe imports `vulkan-1.dll`
  directly and Windows itself does not ship that file — it normally arrives with
  a GPU driver, so a machine without one cannot start mpv at all. The loader is
  taken from the MSYS2 `mingw-w64-x86_64-vulkan-loader` package and its license
  is packaged next to it as `mpv/VULKAN-LOADER-LICENSE.txt`.
- **macOS:** release packaging stages a self-contained mpv from the build host
  (for example Homebrew + dylibbundler).

Corresponding source for those components is available from the upstream
projects and the specific mpv build used for each release.

## Go modules

Direct dependencies:

| Module | License |
|---|---|
| [fyne.io/systray](https://github.com/fyne-io/systray) | Apache-2.0 |
| [github.com/Microsoft/go-winio](https://github.com/microsoft/go-winio) | MIT |
| [github.com/hirochachacha/go-smb2](https://github.com/hirochachacha/go-smb2) | BSD-2-Clause |
| [github.com/itchyny/volume-go](https://github.com/itchyny/volume-go) | MIT |
| [github.com/jchv/go-webview2](https://github.com/jchv/go-webview2) | MIT |
| [github.com/skip2/go-qrcode](https://github.com/skip2/go-qrcode) | MIT |
| [golang.org/x/net](https://pkg.go.dev/golang.org/x/net) | BSD-3-Clause |
| [golang.org/x/text](https://pkg.go.dev/golang.org/x/text) | BSD-3-Clause |

Transitive modules (also pinned in `go.sum`) include, among others:
`github.com/geoffgarside/ber` (BSD), `github.com/go-ole/go-ole` (MIT),
`github.com/godbus/dbus/v5` (BSD-2-Clause), `github.com/jchv/go-winloader` (ISC),
`github.com/moutend/go-wca` (MIT), `golang.org/x/crypto` and `golang.org/x/sys`
(BSD-3-Clause). License texts are in each module’s upstream repository.

## Web UI icons

- **[Lucide Icons](https://lucide.dev)** — MIT. Inline SVG path data in `web/`.

## System components (not redistributed here)

- **Microsoft Edge WebView2 Runtime** (Windows) — installed with Windows / by
  Microsoft; TinyPlay links it at runtime and does not ship the runtime in this
  source tree.

## Desktop imagery

Background images under `internal/server/assets/` are used by the desktop intro
window. No NASA logo is included; use does not imply NASA endorsement. See
[NASA Images and Media](https://www.nasa.gov/nasa-brand-center/images-and-media/).

| Asset | Credit | Source |
|---|---|---|
| `carina_nebula.jpg` | NASA, ESA, CSA, STScI | https://images.nasa.gov/details/carina_nebula |
| `ngc6000.jpg` | ESA/Hubble & NASA, A. Filippenko; ack. M. H. Özsaraç | https://science.nasa.gov/missions/hubble/hubble-studies-star-ages-in-colorful-galaxy/ |
| `earthrise_lro.jpg` | NASA/Goddard Space Flight Center/Arizona State University | https://svs.gsfc.nasa.gov/hyperwall/index/data/events/2018/2018-earthday/thaller/LRO_earthrise.hwshow.html |
