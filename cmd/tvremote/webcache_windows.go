//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
	"tvremote/internal/webcache"
)

// websiteProfileDirName is the profile directory's name under whichever root
// is in effect. Keeping the leaf name fixed means a relocated profile is
// recognisable on disk, and the uninstaller's {userappdata} entry (see
// windows/TinyPlay.iss) still matches the default location.
const websiteProfileDirName = "webview2-website"

// websiteProfileRoot resolves where the built-in browser keeps its WebView2
// profile: the user's relocated directory if it is usable, otherwise the
// default under config.DataDir().
//
// A relocated path fails for mundane reasons — the external drive is unplugged,
// the directory was deleted, permissions changed. None of those justify
// refusing to open a browser window, so an unusable path falls back to the
// default and clears the preference rather than leaving the user with a setting
// that silently does nothing. Same philosophy as a stale custom mpv path.
func websiteProfileRoot() string {
	fallback := filepath.Join(config.DataDir(), websiteProfileDirName)
	custom := config.WebsiteCacheDir()
	if custom == "" {
		return fallback
	}
	if err := validateCacheDir(custom); err != nil {
		log.Printf("webcache: custom location %q unusable (%v); falling back to %s", custom, err, fallback)
		config.SetWebsiteCacheDir("")
		messageBoxOK("TinyPlay", i18n.System("webcache_location_invalid"))
		return fallback
	}
	return filepath.Join(custom, websiteProfileDirName)
}

// validateCacheDir proves the directory exists and can actually be written to.
// Checking os.Stat alone is not enough: a path can exist and still be read-only
// or on a disconnected network share, and finding that out when Chromium fails
// to start gives the user no way to connect the two.
func validateCacheDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	probe, err := os.CreateTemp(dir, ".tinyplay-write-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	probe.Close()
	return os.Remove(name)
}

// enforceWebsiteCacheLimit trims the profile at shell startup, before any
// browser window can exist. It never runs while a window is open — see
// webcache.Clear on why deleting under a live Chromium is worse than a full
// cache.
func enforceWebsiteCacheLimit() {
	limit, capped := config.WebsiteCacheLimitBytes()
	if !capped {
		return
	}
	freed, err := webcache.Enforce(websiteProfileRoot(), limit)
	if err != nil {
		log.Printf("webcache: enforce failed: %v", err)
		return
	}
	if freed > 0 {
		log.Printf("webcache: over the %s budget, reclaimed %s",
			webcache.FormatBytes(limit), webcache.FormatBytes(freed))
	}
}

// websiteCacheClearTitle renders the menu item, e.g. "清除缓存 (1.2 GB)". The
// size is measured every time the title is refreshed rather than cached: this
// mirrors how the autostart checkbox is read back from the OS, and a stale
// number here would be a number the user acts on.
func websiteCacheClearTitle() string {
	used, err := webcache.Usage(websiteProfileRoot())
	if err != nil {
		return i18n.System("webcache_clear")
	}
	return i18n.System("webcache_clear") + " (" + webcache.FormatBytes(used) + ")"
}

// clearWebsiteCache is the menu action. It refuses while the browser window is
// open instead of closing it on the user's behalf: they may be halfway through
// something, and a menu item labelled "clear cache" has no business discarding
// a page.
func clearWebsiteCache() {
	if websiteWindowIsOpen() {
		messageBoxOK("TinyPlay", i18n.System("webcache_busy"))
		return
	}
	freed, err := webcache.Clear(websiteProfileRoot())
	if err != nil {
		log.Printf("webcache: clear failed: %v", err)
	}
	messageBoxOK("TinyPlay", i18n.System("webcache_clear_done")+" ("+webcache.FormatBytes(freed)+")")
}

// chooseWebsiteCacheLocation moves the profile root. The move takes effect the
// next time a browser window opens; the existing profile is deliberately not
// copied across, because the only thing worth carrying is the cache, and the
// cache is the thing the user is trying to get off this drive.
func chooseWebsiteCacheLocation(owner uintptr) {
	if websiteWindowIsOpen() {
		messageBoxOK("TinyPlay", i18n.System("webcache_busy"))
		return
	}
	dir, ok := chooseDirectory(owner, i18n.System("webcache_location_menu"))
	if !ok {
		return
	}
	if err := validateCacheDir(dir); err != nil {
		log.Printf("webcache: chosen location %q unusable: %v", dir, err)
		messageBoxOK("TinyPlay", i18n.System("webcache_location_invalid"))
		return
	}
	config.SetWebsiteCacheDir(dir)
}

// ---- website window liveness -------------------------------------------

// The tray menu and the website shell live in the same process but were built
// independently, so the host is published here rather than threaded through
// every call site. Only liveness is exposed: the menu has no business steering
// the window.
var activeWebsite struct {
	sync.Mutex
	host *websiteHost
}

func publishWebsiteHost(h *websiteHost) {
	activeWebsite.Lock()
	activeWebsite.host = h
	activeWebsite.Unlock()
}

func websiteWindowIsOpen() bool {
	activeWebsite.Lock()
	h := activeWebsite.host
	activeWebsite.Unlock()
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.open
}

// ---- folder picker ------------------------------------------------------

// SHBrowseForFolderW rather than IFileDialog: this shell already talks to the
// classic comdlg32 dialogs directly (see mpv_dialog_windows.go) and the modern
// picker would drag in COM initialisation on the tray's thread for no
// user-visible gain on a "pick a directory" prompt.
// shell32 itself is already declared in shell_windows.go.
var (
	procSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	procCoTaskMemFree        = windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemFree")
)

// browseInfoW mirrors BROWSEINFOW (shlobj_core.h). As with OPENFILENAMEW, the
// field order and widths must match exactly — shell32 reads this memory
// directly.
type browseInfoW struct {
	hwndOwner      uintptr
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

const (
	bifReturnOnlyFSDirs = 0x00000001 // no printers/control panel in the tree
	bifNewDialogStyle   = 0x00000040 // resizable, with a "New Folder" button
)

func chooseDirectory(owner uintptr, title string) (string, bool) {
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return "", false
	}
	display := make([]uint16, windows.MAX_PATH)
	bi := browseInfoW{
		hwndOwner:      owner,
		pszDisplayName: &display[0],
		lpszTitle:      titlePtr,
		ulFlags:        bifReturnOnlyFSDirs | bifNewDialogStyle,
	}
	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", false // cancelled
	}
	// The PIDL is caller-owned; leaking it would leak on every cancel-free trip
	// through this dialog.
	defer procCoTaskMemFree.Call(pidl)

	buf := make([]uint16, maxFilePathBuffer)
	ret, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&buf[0])))
	if ret == 0 {
		return "", false
	}
	return windows.UTF16ToString(buf), true
}
