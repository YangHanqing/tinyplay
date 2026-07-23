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

## Bundled desktop imagery

- **Carina Nebula “Cosmic Cliffs”** (NASA ID: `carina_nebula`) — James Webb Space
  Telescope NIRCam image of the “Cosmic Cliffs” in the Carina Nebula.
  - Credit: **NASA, ESA, CSA, STScI**
  - Source record: https://images.nasa.gov/details/carina_nebula
  - Bundled asset: `desktop-go/internal/server/assets/carina_nebula.jpg`
    (served at `/desktop/background.jpg` for the desktop intro/standby window)
  - NASA image and media guidance: https://www.nasa.gov/nasa-brand-center/images-and-media/
  - This distribution does **not** include a NASA logo and does **not** imply
    NASA endorsement of TinyPlay.
- **NGC 6000** (`Hubble_NGC6000_potw2539a`) — spiral galaxy observed by the
  Hubble Space Telescope.
  - Credit: **ESA/Hubble & NASA, A. Filippenko**; acknowledgment:
    **M. H. Özsaraç**.
  - Source record:
    https://science.nasa.gov/missions/hubble/hubble-studies-star-ages-in-colorful-galaxy/
  - Bundled derivative: `desktop-go/internal/server/assets/ngc6000.jpg`
- **LRO Earthrise** (`earth_and_limb_m1199291564l_color_2stretch_mask_0`) —
  Earth above the lunar horizon, captured by the Lunar Reconnaissance Orbiter.
  - Credit: **NASA/Goddard Space Flight Center/Arizona State University**.
  - Source record:
    https://svs.gsfc.nasa.gov/hyperwall/index/data/events/2018/2018-earthday/thaller/LRO_earthrise.hwshow.html
  - Bundled derivative: `desktop-go/internal/server/assets/earthrise_lro.jpg`
- These assets contain no NASA logo and do not imply NASA endorsement of
  TinyPlay. Their use remains subject to NASA's image and media guidance.

## Go source dependencies

- fyne.io/systray
- github.com/Microsoft/go-winio
- github.com/jchv/go-webview2
- github.com/skip2/go-qrcode
- their transitive dependencies recorded in `desktop-go/go.sum`

Do not treat this inventory as a substitute for shipping the required license
texts with binary releases.
