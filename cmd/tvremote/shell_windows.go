//go:build windows

package main

import (
	"context"
	_ "embed"
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
// Its menu has exactly two items: open the intro/QR window in WebView2, and
// quit. The mpv player window only appears when the user triggers
// playback from their phone.
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
		mAuto := mLanguage.AddSubMenuItemCheckbox(i18n.System("language_auto"), "", selected == "auto")
		mChinese := mLanguage.AddSubMenuItemCheckbox(i18n.System("language_chinese"), "", selected == "zh-CN")
		mEnglish := mLanguage.AddSubMenuItemCheckbox("English", "", selected == "en")
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
			mAuto.SetTitle(i18n.System("language_auto"))
			mChinese.SetTitle(i18n.System("language_chinese"))
			mAbout.SetTitle(i18n.System("about"))
			mAbout.SetTooltip(i18n.System("about_tip"))
			mQuit.SetTitle(i18n.System("quit"))
			mQuit.SetTooltip(i18n.System("quit_tip"))
			systray.SetTooltip(i18n.System("tooltip"))
			for value, item := range map[string]*systray.MenuItem{"auto": mAuto, "zh-CN": mChinese, "en": mEnglish} {
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
				case <-mAuto.ClickedCh:
					applyLanguage("auto")
				case <-mChinese.ClickedCh:
					applyLanguage("zh-CN")
				case <-mEnglish.ClickedCh:
					applyLanguage("en")
				}
			}
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
	w.Navigate(url)
	w.Run()
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
	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

const (
	mbYesNo           = 0x00000004
	mbIconInformation = 0x00000040
	idYes             = 6
)

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
