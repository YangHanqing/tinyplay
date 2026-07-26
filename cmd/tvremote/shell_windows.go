//go:build windows

package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
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

// feedbackMailAddress is the product support inbox. Keep it in one place so the
// tray "Send Feedback" action and any future shell entry points stay aligned.
const feedbackMailAddress = "tinyday.app@gmail.com"

// runShell on Windows: a tray icon that sits silently in the notification area.
// The mpv player window only appears when the user triggers playback from
// their phone.
//
// The tray menu is intentionally small: open the main QR window, pick a
// language, send feedback, and quit. Logs / updates / about / DLNA live on the
// main window (and phone settings where relevant) so the tray does not mirror
// the whole surface.
func runShell(localURL string, httpSrv *http.Server) {
	// Every shell→core call goes over loopback. /desktop/* is loopback-only
	// because the QR image carries the pairing secret, and /api/* needs a device
	// token from anyone who is not on this machine. The LAN address stays a
	// display detail, rendered by the core on the /desktop page itself.
	coreURL := loopbackCoreURL(localURL)
	desktopURL := func() string {
		return coreURL + "/desktop?lang=" + url.QueryEscape(i18n.SystemLang()) + "&version=" + url.QueryEscape(version)
	}

	onReady := func() {
		// Runs separately from the website WebView: the temporary mouse is a
		// computer-wide, foreground-window control, not a website-only feature.
		startDesktopInputHost()
		// A phone that typed the address by hand needs somebody to approve it on
		// this computer, and the card lives in a window that is normally closed.
		// See watchPairingRequests.
		go watchPairingRequests(context.Background(), coreURL, func() {
			openWindow(desktopURL())
		})
		systray.SetIcon(trayIcon)
		systray.SetTitle("TinyPlay")
		systray.SetTooltip(i18n.System("tooltip"))

		mOpen := systray.AddMenuItem(i18n.System("open_main"), i18n.System("open_main_tip"))
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
		systray.AddSeparator()
		mFeedback := systray.AddMenuItem(i18n.System("feedback"), i18n.System("feedback_tip"))
		systray.AddSeparator()
		mQuit := systray.AddMenuItem(i18n.System("quit"), i18n.System("quit_tip"))

		applyLanguage := func(language string) {
			config.SetLanguage(language)
			mOpen.SetTitle(i18n.System("open_main"))
			mOpen.SetTooltip(i18n.System("open_main_tip"))
			mLanguage.SetTitle(i18n.System("language"))
			languageItems["auto"].SetTitle(i18n.System("language_auto"))
			mFeedback.SetTitle(i18n.System("feedback"))
			mFeedback.SetTooltip(i18n.System("feedback_tip"))
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
				case <-mFeedback.ClickedCh:
					openFeedbackEmail()
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
				}
			}
		}()

		// Website playback shell: dedicated singleton WebView2, separate from QR.
		go startWebsiteShell(coreURL)
		// "Connecting…" / next-episode-countdown indicator; see toast_windows.go.
		go startToastShell(coreURL)
		// Update discovery is deliberately late and asynchronous: a slow or
		// blocked GitHub connection must never delay the tray or QR window.
		go func() {
			time.Sleep(8 * time.Second)
			checkForTinyPlayUpdates(false)
		}()

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

// openFeedbackEmail launches the user's default mail client with a prefilled
// support address and a short prompt: feature ideas can be free-form; bugs
// should include the app logs from the main window.
func openFeedbackEmail() {
	q := url.Values{}
	q.Set("subject", i18n.System("feedback_mail_subject"))
	q.Set("body", i18n.System("feedback_mail_body"))
	openWithDefaultHandler("mailto:" + feedbackMailAddress + "?" + q.Encode())
}

var (
	windowMu sync.Mutex

	// Handle of the live QR window, so a second tray click can raise the
	// existing one instead of doing nothing. Guarded separately from windowMu,
	// which is held for the whole lifetime of the window's message loop.
	mainWindowMu   sync.Mutex
	mainWindowHWND uintptr
)

func setMainWindowHWND(hwnd uintptr) {
	mainWindowMu.Lock()
	mainWindowHWND = hwnd
	mainWindowMu.Unlock()
}

// openWindow shows the intro/QR page in a WebView2 window. Only one window is
// allowed at a time; the call blocks (on its own OS-locked thread) until the
// window closes.
func openWindow(url string) {
	if !windowMu.TryLock() {
		// Already open — raise it rather than returning silently, which makes
		// the tray item look dead whenever the window is buried behind
		// something else. Mirrors the macOS shell's openMainWindow().
		mainWindowMu.Lock()
		hwnd := mainWindowHWND
		mainWindowMu.Unlock()
		bringWindowToFront(hwnd)
		return
	}
	defer windowMu.Unlock()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug: false,
		WindowOptions: webview2.WindowOptions{
			Title: "TinyPlay",
			// Outer window size, not the WebView's content area — the title
			// bar eats a slice of the height (see desktop.go's compact-page
			// comment). 900x540 is the content area the compact page's CSS
			// targets; the +36 below is a rough allowance for that title bar.
			Width:  900,
			Height: 576,
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
	setMainWindowHWND(hwnd)
	defer setMainWindowHWND(0)

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
			// The compact QR window intentionally has no minimize affordance.
			// winFullscreen is also used by the normal website window, so keep
			// that window-style policy at this call site rather than in exit().
			removeMinimizeButton(hwnd)
		}
		notifyJS(enter)
		return nil
	})
	// Lets a reloaded page rediscover borderless standby after DLNA/lang refresh.
	_ = w.Bind("tinyplayIsFullscreen", func() (bool, error) {
		return fs.active, nil
	})
	// Footer bridges — same names as macOS messageHandlers / page JS:
	//   tinyplayShowAbout, tinyplayCheckForUpdates (+ __tinyplayUpdateCheckDone)
	// Check-for-updates runs on this call (not a background goroutine) so the
	// page can await the Bind promise; we also post the shared done callback so
	// both shells clear the spinner the same way.
	_ = w.Bind("tinyplayCheckForUpdates", func() error {
		checkForTinyPlayUpdates(true)
		w.Eval(`window.__tinyplayUpdateCheckDone && window.__tinyplayUpdateCheckDone()`)
		return nil
	})
	_ = w.Bind("tinyplayShowAbout", func() error {
		showAbout()
		return nil
	})

	w.Navigate(url)
	w.Run()
}

// winFullscreen tracks a window's normal style/rect so a temporary borderless
// monitor fullscreen can reverse cleanly without recreating the WebView2 host.
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
	newStyle := (style &^ (wsCaption | wsThickFrame | wsSysMenu | wsMinimizeBox | wsMaximizeBox | wsMaximize)) | wsPopup | wsVisible
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
	user32                       = syscall.NewLazyDLL("user32.dll")
	comctl32                     = syscall.NewLazyDLL("comctl32.dll")
	shell32                      = syscall.NewLazyDLL("shell32.dll")
	procMessageBoxW              = user32.NewProc("MessageBoxW")
	procFindWindowW              = user32.NewProc("FindWindowW")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procMonitorFromWindow        = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procShowWindow               = user32.NewProc("ShowWindow")
	procIsIconic                 = user32.NewProc("IsIconic")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procTaskDialogIndirect       = comctl32.NewProc("TaskDialogIndirect")
	procShellExecuteW            = shell32.NewProc("ShellExecuteW")
)

// hwndNoTopmost is HWND_NOTOPMOST (-2): drops a window out of the always-on-top
// band and back to the head of the normal one. Paired with hwndTopmost (defined
// in toast_windows.go) it is the standard way to raise a window without the
// foreground lock having a say — see bringWindowToFront.
var hwndNoTopmost = ^uintptr(1)

const (
	mbYesNo                 = 0x00000004
	mbIconInformation       = 0x00000040
	idYes                   = 6
	tdfSizeToContent        = 0x01000000
	tdfAllowCancellation    = 0x0008
	updateDownloadButton    = 100
	updateRemindButton      = 101
	updateSkipButton        = 102
	gwlStyle                = ^uintptr(15) // GWL_STYLE (-16)
	wsPopup                 = 0x80000000
	wsVisible               = 0x10000000
	wsCaption               = 0x00C00000
	wsThickFrame            = 0x00040000
	wsSysMenu               = 0x00080000
	wsMinimizeBox           = 0x00020000
	wsMaximizeBox           = 0x00010000
	wsMaximize              = 0x01000000
	swpNoSize               = 0x0001
	swpNoMove               = 0x0002
	swpNoZOrder             = 0x0004
	swpNoActivate           = 0x0010
	swpShowWindow           = 0x0040
	swpFrameChanged         = 0x0020
	swShowMaximized         = 3 // SW_SHOWMAXIMIZED
	swRestore               = 9 // SW_RESTORE
	hwndTop                 = 0
	monitorDefaultToNearest = 2
)

// taskDialogConfig mirrors TASKDIALOGCONFIG from commctrl.h. TaskDialog gives
// the update prompt three clearly labelled choices; MessageBox only offers
// fixed Yes/No/Cancel labels, which makes "skip this version" ambiguous.
type taskDialogConfig struct {
	cbSize                  uint32
	hwndParent              uintptr
	hInstance               uintptr
	dwFlags                 uint32
	dwCommonButtons         uint32
	pszWindowTitle          *uint16
	hMainIcon               uintptr
	pszMainInstruction      *uint16
	pszContent              *uint16
	cButtons                uint32
	pButtons                *taskDialogButton
	nDefaultButton          int32
	cRadioButtons           uint32
	pRadioButtons           *taskDialogButton
	nDefaultRadioButton     int32
	pszVerificationText     *uint16
	pszExpandedInformation  *uint16
	pszExpandedControlText  *uint16
	pszCollapsedControlText *uint16
	hFooterIcon             uintptr
	pszFooter               *uint16
	pfCallback              uintptr
	lpCallbackData          uintptr
	cxWidth                 uint32
}

type taskDialogButton struct {
	nButtonID     int32
	pszButtonText *uint16
}

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

// bringWindowToFront raises a shell window above everything else and gives it
// input focus. Every window here is opened by a phone tap, on a background
// thread, with no local input event behind it — exactly the case Windows'
// foreground rules are written to refuse, so the implicit activation that
// ShowWindow would normally perform is denied and the window is left *behind*
// the currently active one (typically our own QR window). The macOS shell never
// hit this because it always asks explicitly: makeKeyAndOrderFront +
// NSApp.activate(ignoringOtherApps:) on both the create and the reuse path.
//
// Two steps, because they fix two different things and only one of them can
// fail:
//  1. HWND_TOPMOST then HWND_NOTOPMOST. Z-order is not subject to the
//     foreground lock, so this alone guarantees the window is no longer
//     covered. It does not leave the window always-on-top — the second call
//     drops it back into the normal band, now at its head.
//  2. Attaching to the foreground window's input queue lifts the lock for the
//     duration of SetForegroundWindow, so the window also takes keyboard and
//     mouse focus. Best-effort: if it is refused, step 1 still stands and the
//     window is at least visible.
func bringWindowToFront(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	// Only when actually minimized: SW_RESTORE on a maximized window would
	// shrink the browsing window back to its pre-maximize size.
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		_, _, _ = procShowWindow.Call(hwnd, uintptr(swRestore))
	}
	const posFlags = uintptr(swpNoMove | swpNoSize | swpShowWindow)
	_, _, _ = procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, posFlags)
	_, _, _ = procSetWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, posFlags)

	target, _, _ := procGetWindowThreadProcessId.Call(hwnd, 0)
	var current uintptr
	if foreground, _, _ := procGetForegroundWindow.Call(); foreground != 0 && foreground != hwnd {
		current, _, _ = procGetWindowThreadProcessId.Call(foreground, 0)
	}
	if current != 0 && target != 0 && current != target {
		_, _, _ = procAttachThreadInput.Call(current, target, 1)
		defer procAttachThreadInput.Call(current, target, 0)
	}
	_, _, _ = procSetForegroundWindow.Call(hwnd)
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

func checkForTinyPlayUpdates(manual bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	release, err := findTinyPlayUpdate(ctx, version)
	if err != nil {
		log.Printf("Update check failed: %v", err)
		if manual {
			showInformation(i18n.System("update_failed"))
		}
		return
	}
	if release == nil {
		if manual {
			showInformation(i18n.System("update_latest"))
		}
		return
	}
	if !manual && !config.ShouldOfferUpdate(release.Version, time.Now()) {
		return
	}

	switch showUpdateDialog(release.Version) {
	case updateDownloadButton:
		openWithDefaultHandler(release.PageURL)
	case updateRemindButton:
		config.RemindAboutUpdateAfter(release.Version, time.Now().Add(72*time.Hour))
	case updateSkipButton:
		config.SkipUpdate(release.Version)
	}
}

func showInformation(text string) {
	_, _, _ = procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("TinyPlay"))),
		uintptr(mbIconInformation),
	)
}

func showUpdateDialog(latestVersion string) int {
	buttons := []taskDialogButton{
		{nButtonID: updateDownloadButton, pszButtonText: syscall.StringToUTF16Ptr(i18n.System("update_download"))},
		{nButtonID: updateRemindButton, pszButtonText: syscall.StringToUTF16Ptr(i18n.System("update_remind"))},
		{nButtonID: updateSkipButton, pszButtonText: syscall.StringToUTF16Ptr(i18n.System("update_skip"))},
	}
	config := taskDialogConfig{
		cbSize:             uint32(unsafe.Sizeof(taskDialogConfig{})),
		dwFlags:            tdfSizeToContent | tdfAllowCancellation,
		pszWindowTitle:     syscall.StringToUTF16Ptr("TinyPlay"),
		pszMainInstruction: syscall.StringToUTF16Ptr(i18n.System("update_available_title")),
		pszContent:         syscall.StringToUTF16Ptr(i18n.System("update_available_body", latestVersion, version)),
		cButtons:           uint32(len(buttons)),
		pButtons:           &buttons[0],
		nDefaultButton:     updateDownloadButton,
	}
	var clicked int32
	result, _, _ := procTaskDialogIndirect.Call(
		uintptr(unsafe.Pointer(&config)),
		uintptr(unsafe.Pointer(&clicked)),
		0,
		0,
	)
	if int32(result) != 0 {
		log.Printf("Update dialog failed: HRESULT %#x", result)
		return 0
	}
	return int(clicked)
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
// associates it with (a browser for URLs, the registered app for a file path;
// mailto: URLs go to whatever the user has set as their mailto handler, which
// is usually a desktop mail client but can be a webmail handler if they
// configured one).
//
// This calls ShellExecuteW directly rather than shelling out via "cmd /C
// start": Go's argv escaping for Windows only quotes an argument when it
// contains a space or tab (see syscall.EscapeArg), so a target with an
// unquoted "&" — e.g. a mailto: URL with more than one query parameter, or a
// GitHub release URL with a query string — reaches cmd.exe unquoted and gets
// split into two commands there, silently truncating the target and running
// the remainder as a bogus command.
func openWithDefaultHandler(target string) {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return
	}
	path, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return
	}
	const swShowNormal = 1
	_, _, _ = procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(path)), 0, 0, swShowNormal)
}
