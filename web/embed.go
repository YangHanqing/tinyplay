// Package web embeds TinyPlay's phone-facing frontend so the core binary is
// self-contained.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js styles.css i18n.js manifest.webmanifest sw.js favicon.ico favicon-16.png favicon-32.png apple-touch-icon.png pwa-icon-192.png pwa-icon-512.png pwa-maskable-192.png pwa-maskable-512.png
var embedded embed.FS

// FS returns the frontend file tree rooted at this package directory, so
// "index.html", "app.js", and "styles.css" are at the root.
func FS() fs.FS { return embedded }
