// Package webcache bounds the built-in browser's on-disk profile.
//
// Every other directory under config.DataDir() has a ceiling of its own: the
// core log rotates at 5MB with three backups, artwork is trimmed to
// imagecache.MaxBytes. The WebView profile has none, and it is filled by video
// sites that cache aggressively, so it is the one thing here that can quietly
// grow into gigabytes on a system drive.
//
// The hard rule in this package is that clearing the cache must NOT sign the
// user out. A Chromium profile mixes two kinds of data in one tree:
//
//	derived   Cache/, Code Cache/, GPUCache/, Service Worker/CacheStorage/, …
//	          — refetched on demand, safe to delete
//	user      Network/Cookies, Local Storage/, IndexedDB/, Session Storage/, …
//	          — logins, tokens and site preferences
//
// So this package deletes by an explicit allow-list of derived directory names
// and never by "everything under the profile root". A name that is not on the
// list is left alone even if it looks like cache, because the cost of the two
// mistakes is not symmetric: keeping some junk costs disk, deleting the wrong
// directory costs the user their logins.
//
// WebView2 exposes ICoreWebView2Profile::ClearBrowsingData for exactly this,
// but github.com/jchv/go-webview2 does not bind it, so we work on the directory
// tree instead. That makes the layout below an assumption about Chromium rather
// than a contract: if a future runtime renames these directories the sweep
// quietly reclaims less, which is the safe direction to fail in.
package webcache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// derivedDirs are the profile subdirectory names that hold only refetchable
// data. Matched by exact name at any depth, because the profile root ("Default"
// vs a numbered profile) and the runtime's own wrapper directory ("EBWebView")
// are both things we would rather not hard-code a path through.
//
// Deliberately absent, and not to be added without re-reading the package doc:
// Network (holds Cookies), Local Storage, Session Storage, IndexedDB,
// databases, Login Data, Preferences, Service Worker/Database.
var derivedDirs = map[string]bool{
	"Cache":                true,
	"Code Cache":           true,
	"GPUCache":             true,
	"DawnCache":            true,
	"DawnGraphiteCache":    true,
	"DawnWebGPUCache":      true,
	"ShaderCache":          true,
	"GrShaderCache":        true,
	"GraphiteDawnCache":    true,
	"CacheStorage":         true, // Service Worker/CacheStorage — the video-site bulk
	"ScriptCache":          true, // Service Worker/ScriptCache
	"Storage Cache":        true,
	"component_crx_cache":  true,
	"extensions_crx_cache": true,
}

// Usage reports how many bytes Clear would actually reclaim — not the size of
// the whole profile. The tray menu renders this number next to a "Clear cache"
// item, so counting cookies and local storage there would promise a saving the
// action does not deliver.
//
// A missing root is 0 bytes and no error: the profile is created lazily on the
// first browser window, so "never opened" is an ordinary state, not a failure.
func Usage(root string) (int64, error) {
	dirs, err := findDerivedDirs(root)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, dir := range dirs {
		size, err := dirSize(dir)
		if err != nil {
			// One unreadable subtree should not hide the rest of the total.
			continue
		}
		total += size
	}
	return total, nil
}

// Clear removes the derived directories and reports the bytes reclaimed.
//
// The caller must ensure the browser window is closed first. Deleting cache
// files under a live Chromium leaves it with an index pointing at files that no
// longer exist, which is a far worse outcome than a full cache; the shells
// therefore gate this on the window being shut rather than trying to be clever.
func Clear(root string) (int64, error) {
	dirs, err := findDerivedDirs(root)
	if err != nil {
		return 0, err
	}
	var freed int64
	var failed []string
	for _, dir := range dirs {
		size, _ := dirSize(dir)
		if err := os.RemoveAll(dir); err != nil {
			failed = append(failed, filepath.Base(dir))
			continue
		}
		freed += size
	}
	if len(failed) > 0 {
		return freed, fmt.Errorf("webcache: could not remove %s", strings.Join(failed, ", "))
	}
	return freed, nil
}

// Enforce clears the cache when it exceeds limit, and reports the bytes freed
// (0 when the cache was already within budget).
//
// It clears wholesale rather than evicting the least-recently-used files.
// Chromium's cache directories carry their own index; removing individual files
// underneath it produces an inconsistent store, whereas removing a whole cache
// directory is a state the runtime already handles — it is what a first launch
// looks like. Callers run this at startup, with no browser window open.
func Enforce(root string, limit int64) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	used, err := Usage(root)
	if err != nil {
		return 0, err
	}
	if used <= limit {
		return 0, nil
	}
	return Clear(root)
}

// findDerivedDirs walks the profile and returns the top-most directory for each
// allow-listed name. Top-most matters: once a directory is going to be removed
// there is no reason to descend into it, and a nested match would otherwise be
// counted twice by Usage.
func findDerivedDirs(root string) ([]string, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("webcache: empty profile root")
	}
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree is skipped, not fatal: a locked directory
			// somewhere in the profile must not make the menu item unusable.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() || path == root {
			return nil
		}
		if derivedDirs[d.Name()] {
			found = append(found, path)
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}

// FormatBytes renders a size for a menu title, e.g. "1.2 GB". Units are left
// untranslated on purpose — they read the same in every language this app
// ships, and a localized unit would have to be threaded through i18n for no
// gain.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 3; v /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := [...]string{"KB", "MB", "GB", "TB"}[exp]
	// One decimal below 10 keeps "1.2 GB" informative without making "934 MB"
	// pointlessly precise.
	if value < 10 {
		return fmt.Sprintf("%.1f %s", value, suffix)
	}
	return fmt.Sprintf("%.0f %s", value, suffix)
}
