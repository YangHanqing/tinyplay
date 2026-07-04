// Package webui embeds the phone-facing frontend so the binary is
// self-contained.
//
// The frontend's single source of truth is the repo-root web/ directory (shared
// with the Python branch). go:embed cannot reference paths outside the module,
// so the files are synced into ./dist before building:
//
//	make sync   # copies ../../web/* into internal/webui/dist/
//
// dist/ is git-ignored except for .gitkeep. Run `make sync` (or the CI build
// step) before `go build`, or the server will serve an empty page.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the frontend file tree rooted at dist/ (so "index.html",
// "app.js", "styles.css" are at the root).
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
