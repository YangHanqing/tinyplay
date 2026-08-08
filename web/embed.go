// Package web embeds TinyPlay's phone-facing frontend, keeping the core binary
// self-contained for desktop distribution.
package web

import (
	"embed"
	"io/fs"
)

// nowplaying.png is not part of the phone UI: it is the artwork mpv displays
// in place of the missing video track when a song is playing. It lives here
// because the embedded FS is already served over the LAN port, which is the
// one place both the desktop core and mpv can reach it from.
//
// It is 1920x1080 and must stay 16:9. mpv treats cover art as the video
// stream, so the file's own aspect ratio is the shape of the player window —
// the earlier square asset is why the window opened square and pillarboxed on
// every ordinary display. It is also wordless on purpose: one asset ships to
// every locale, so any text would need translating and would bake a font into
// a PNG.
//
//go:embed index.html app.js styles.css i18n.js manifest.webmanifest sw.js favicon.ico favicon-16.png favicon-32.png apple-touch-icon.png pwa-icon-192.png pwa-icon-512.png pwa-maskable-192.png pwa-maskable-512.png nowplaying.png
var embedded embed.FS

// FS returns the frontend file tree rooted at this package directory, so
// "index.html", "app.js", and "styles.css" are at the root.
func FS() fs.FS { return embedded }
