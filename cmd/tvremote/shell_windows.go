//go:build windows

package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/systray"
	webview2 "github.com/jchv/go-webview2"

	"tvremote/internal/config"
	"tvremote/internal/i18n"
)

//go:embed icon.ico
var trayIcon []byte

// runShell on Windows: a tray icon that sits silently in the notification area.
// The mpv player window only appears when the user triggers playback from
// their phone.
func runShell(localURL string, httpSrv *http.Server) {
	desktopURL := func() string { return localURL + "/desktop?lang=" + url.QueryEscape(i18n.SystemLang()) }

	onReady := func() {
		systray.SetIcon(trayIcon)
		systray.SetTitle("TinyPlay")
		systray.SetTooltip(i18n.System("tooltip"))

		mOpen := systray.AddMenuItem(i18n.System("open_main"), i18n.System("open_main_tip"))
		mLogs := systray.AddMenuItem(i18n.System("open_logs"), i18n.System("open_logs_tip"))
		mLanguage := systray.AddMenuItem(i18n.System("language"), "")
		selected := config.Load().Language
		languageNames := []struct{ value, title string }{
			{"auto", i18n.System("language_auto")}, {"en", "English"},
			{"zh-CN", "简体中文"}, {"zh-TW", "繁體中文"}, {"ja", "日本語"},
			{"ko", "한국어"}, {"es", "Español"}, {"fr", "Français"}, {"de", "Deutsch"},
		}
		languageItems := make(map[string]*systray.MenuItem, len(languageNames))
		for _, entry := range languageNames {
			languageItems[entry.value] = mLanguage.AddSubMenuItemCheckbox(entry.title, "", selected == entry.value)
		}
		mSettings := systray.AddMenuItem(i18n.System("settings"), "")
		dlnaEnabled := config.Load().DLNAReceiverEnabled
		mDLNA := mSettings.AddSubMenuItemCheckbox(i18n.System("dlna_receiver"), i18n.System("dlna_receiver_tip"), dlnaEnabled)
		systray.AddSeparator()
		mAbout := systray.AddMenuItem(i18n.System("about"), i18n.System("about_tip"))
		mQuit := systray.AddMenuItem(i18n.System("quit"), i18n.System("quit_tip"))

		applyLanguage := func(language string) {
			config.SetLanguage(language)
			mOpen.SetTitle(i18n.System("open_main"))
			mOpen.SetTooltip(i18n.System("open_main_tip"))
			mLogs.SetTitle(i18n.System("open_logs"))
			mLogs.SetTooltip(i18n.System("open_logs_tip"))
			mLanguage.SetTitle(i18n.System("language"))
			languageItems["auto"].SetTitle(i18n.System("language_auto"))
			mSettings.SetTitle(i18n.System("settings"))
			mDLNA.SetTitle(i18n.System("dlna_receiver"))
			mDLNA.SetTooltip(i18n.System("dlna_receiver_tip"))
			mAbout.SetTitle(i18n.System("about"))
			mAbout.SetTooltip(i18n.System("about_tip"))
			mQuit.SetTitle(i18n.System("quit"))
			mQuit.SetTooltip(i18n.System("quit_tip"))
			systray.SetTooltip(i18n.System("tooltip"))
			for value, item := range languageItems {
				if value == config.NormalizeLanguage(language) {
					item.Check()
				} else {
					item.Uncheck()
				}
			}
		}

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					openWindow(desktopURL())
				case <-mAbout.ClickedCh:
					showAbout()
				case <-mLogs.ClickedCh:
					if resp, err := http.Get(localURL + "/desktop/open-logs"); err == nil {
						resp.Body.Close()
					}
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				case <-languageItems["auto"].ClickedCh:
					applyLanguage("auto")
				case <-languageItems["en"].ClickedCh:
					applyLanguage("en")
				case <-languageItems["zh-CN"].ClickedCh:
					applyLanguage("zh-CN")
				case <-languageItems["zh-TW"].ClickedCh:
					applyLanguage("zh-TW")
				case <-languageItems["ja"].ClickedCh:
					applyLanguage("ja")
				case <-languageItems["ko"].ClickedCh:
					applyLanguage("ko")
				case <-languageItems["es"].ClickedCh:
					applyLanguage("es")
				case <-languageItems["fr"].ClickedCh:
					applyLanguage("fr")
				case <-languageItems["de"].ClickedCh:
					applyLanguage("de")
				case <-mDLNA.ClickedCh:
					next := !dlnaEnabled
					if setDLNAReceiverEnabled(localURL, next) {
						dlnaEnabled = next
						if next {
							mDLNA.Check()
						} else {
							mDLNA.Uncheck()
						}
					}
				}
			}
		}()

		// Website playback shell: dedicated full-screen WebView2, separate from QR.
		go startWebsiteShell(localURL)

		// Show the window once on first launch so users see the QR immediately.
		go openWindow(desktopURL())
	}

	onExit := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}

	systray.Run(onReady, onExit)
}

// setDLNAReceiverEnabled goes through the core's settings endpoint so a tray
// click updates both persisted configuration and the live SSDP socket.
func setDLNAReceiverEnabled(localURL string, enabled bool) bool {
	body, err := json.Marshal(map[string]bool{"dlna_receiver_enabled": enabled})
	if err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodPut, localURL+"/api/settings", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices
}

var windowMu sync.Mutex

// openWindow shows the intro/QR page in a WebView2 window. Only one window is
// allowed at a time; the call blocks (on its own OS-locked thread) until the
// window closes.
func openWindow(url string) {
	if !windowMu.TryLock() {
		return // already open
	}
	defer windowMu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug: false,
		WindowOptions: webview2.WindowOptions{
			Title:  "TinyPlay",
			Width:  360,
			Height: 560,
			Center: true,
		},
	})
	if w == nil {
		showWebView2Missing()
		return
	}
	defer w.Destroy()
	// This is a short-lived QR window, not a document window. Closing it keeps
	// TinyPlay running in the tray, so a separate Minimize state only leaves an
	// otherwise hidden window for the user to recover later.
	hwnd := uintptr(w.Window())
	removeMinimizeButton(hwnd)

	// Borderless full-screen on the monitor that currently hosts this window.
	// Preserve style + rectangle so Exit / Escape restores the compact QR size.
	var fs winFullscreen
	notifyJS := func(enter bool) {
		flag := "false"
		if enter {
			flag = "true"
		}
		w.Eval(`window.__tinyplayNativeFullscreen && window.__tinyplayNativeFullscreen(` + flag + `)`)
	}
	_ = w.Bind("tinyplaySetFullscreen", func(enter bool) error {
		hwnd := uintptr(w.Window())
		if hwnd == 0 {
			return nil
		}
		if enter {
			if err := fs.enter(hwnd); err != nil {
				return err
			}
		} else {
			fs.exit(hwnd)
		}
		notifyJS(enter)
		return nil
	})
	// Lets a reloaded page rediscover borderless standby after DLNA/lang refresh.
	_ = w.Bind("tinyplayIsFullscreen", func() (bool, error) {
		return fs.active, nil
	})

	w.Navigate(url)
	w.Run()
}

// winFullscreen tracks the compact-window style/rect so borderless HTPC mode
// can reverse cleanly without recreating the WebView2 host.
type winFullscreen struct {
	active bool
	style  uintptr
	rect   winRect
}

type winRect struct {
	Left, Top, Right, Bottom int32
}

type winMonitorInfo struct {
	Size    uint32
	Monitor winRect
	Work    winRect
	Flags   uint32
}

func (fs *winFullscreen) enter(hwnd uintptr) error {
	if fs.active || hwnd == 0 {
		return nil
	}
	style, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlStyle)
	var r winRect
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ok == 0 {
		return fmt.Errorf("GetWindowRect failed")
	}
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if mon == 0 {
		return fmt.Errorf("MonitorFromWindow failed")
	}
	var mi winMonitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	ok, _, _ = procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	if ok == 0 {
		return fmt.Errorf("GetMonitorInfo failed")
	}

	fs.style = style
	fs.rect = r
	fs.active = true

	// Drop chrome; keep visible. WS_POPUP gives a true borderless surface.
	newStyle := (style &^ (wsCaption | wsThickFrame | wsSysMenu | wsMinimizeBox | wsMaximizeBox)) | wsPopup | wsVisible
	_, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlStyle, newStyle)
	w := mi.Monitor.Right - mi.Monitor.Left
	h := mi.Monitor.Bottom - mi.Monitor.Top
	_, _, _ = procSetWindowPos.Call(hwnd, hwndTop,
		uintptr(mi.Monitor.Left), uintptr(mi.Monitor.Top),
		uintptr(w), uintptr(h),
		uintptr(swpShowWindow|swpFrameChanged))
	return nil
}

func (fs *winFullscreen) exit(hwnd uintptr) {
	if !fs.active || hwnd == 0 {
		return
	}
	_, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlStyle, fs.style)
	w := fs.rect.Right - fs.rect.Left
	h := fs.rect.Bottom - fs.rect.Top
	_, _, _ = procSetWindowPos.Call(hwnd, 0,
		uintptr(fs.rect.Left), uintptr(fs.rect.Top),
		uintptr(w), uintptr(h),
		uintptr(swpNoZOrder|swpShowWindow|swpFrameChanged))
	// Re-apply the no-minimize chrome tweak on the restored frame.
	removeMinimizeButton(hwnd)
	fs.active = false
}

// webview2DownloadURL is Microsoft's official evergreen bootstrapper for the
// WebView2 Runtime; it always redirects to the latest installer.
const webview2DownloadURL = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

// showWebView2Missing tells the user the window can't be shown because the
// WebView2 Runtime isn't installed, and offers to open the download page.
// Most Windows 10/11 machines already have it (it ships with Edge and via
// Windows Update), so this should only fire on a stripped-down install.
func showWebView2Missing() {
	if messageBoxYesNo("TinyPlay", i18n.System("webview2_missing")) {
		openWithDefaultHandler(webview2DownloadURL)
	}
}

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW       = user32.NewProc("MessageBoxW")
	procFindWindowW       = user32.NewProc("FindWindowW")
	procGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
	procGetWindowRect     = user32.NewProc("GetWindowRect")
	procMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
	procShowWindow        = user32.NewProc("ShowWindow")
)

const (
	mbYesNo                 = 0x00000004
	mbIconInformation       = 0x00000040
	idYes                   = 6
	gwlStyle                = ^uintptr(15) // GWL_STYLE (-16)
	wsPopup                 = 0x80000000
	wsVisible               = 0x10000000
	wsCaption               = 0x00C00000
	wsThickFrame            = 0x00040000
	wsSysMenu               = 0x00080000
	wsMinimizeBox           = 0x00020000
	wsMaximizeBox           = 0x00010000
	swpNoSize               = 0x0001
	swpNoMove               = 0x0002
	swpNoZOrder             = 0x0004
	swpNoActivate           = 0x0010
	swpShowWindow           = 0x0040
	swpFrameChanged         = 0x0020
	swShowMaximized         = 3 // SW_SHOWMAXIMIZED
	hwndTop                 = 0
	monitorDefaultToNearest = 2
)

// maximizeWindow shows a normal app window maximized: it fills the work area
// but keeps the system title bar (close / maximize buttons) and the taskbar
// visible. Used for the website browser window, which is an ordinary window the
// user can close natively — not a borderless kiosk surface. Per-site video
// "fullscreen" is handled inside the page by the site's own player, so the host
// window never needs to go borderless.
func maximizeWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	_, _, _ = procShowWindow.Call(hwnd, uintptr(swShowMaximized))
}

// removeMinimizeButton adjusts the native WebView2 host window after the
// dependency has created it. go-webview2 exposes size and title options but
// not title-bar flags. Prefer the HWND from WebView.Window(); fall back to a
// class/title lookup only if the handle is not ready yet.
func removeMinimizeButton(hwnd uintptr) {
	if hwnd == 0 {
		className := syscall.StringToUTF16Ptr("webview")
		title := syscall.StringToUTF16Ptr("TinyPlay")
		found, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)))
		hwnd = found
	}
	if hwnd == 0 {
		return
	}
	style, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlStyle)
	if style&wsMinimizeBox == 0 {
		return
	}
	_, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlStyle, style&^uintptr(wsMinimizeBox))
	_, _, _ = procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0,
		uintptr(swpNoSize|swpNoMove|swpNoZOrder|swpNoActivate|swpFrameChanged))
}

// messageBoxYesNo shows a native Yes/No dialog and reports whether Yes was
// clicked. The button captions themselves follow the OS locale, not the app's.
func messageBoxYesNo(title, text string) bool {
	ret, _, _ := procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		uintptr(mbYesNo|mbIconInformation),
	)
	return ret == idYes
}

// showAbout displays the version (baked in at build time via -ldflags -X
// main.version=...) and offers to open the third-party notices file that
// ships next to TinyPlay.exe.
func showAbout() {
	text := fmt.Sprintf(i18n.System("about_version_line")+"\n\n"+i18n.System("about_view_notices"), version)
	if messageBoxYesNo("TinyPlay", text) {
		openThirdPartyNotices()
	}
}

// openThirdPartyNotices opens THIRD_PARTY_NOTICES.md next to the running exe
// with the user's default handler for .md/text files.
func openThirdPartyNotices() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	path := filepath.Join(filepath.Dir(exe), "THIRD_PARTY_NOTICES.md")
	if _, err := os.Stat(path); err != nil {
		return
	}
	openWithDefaultHandler(path)
}

// openWithDefaultHandler opens a local path or URL with whatever the OS
// associates it with (a browser for URLs, the registered app for a file path).
func openWithDefaultHandler(target string) {
	_ = exec.Command("cmd", "/C", "start", "", target).Start()
}
