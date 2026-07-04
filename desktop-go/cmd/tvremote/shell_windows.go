//go:build windows

package main

import (
	"context"
	_ "embed"
	"net/http"
	"runtime"
	"sync"
	"time"

	"fyne.io/systray"
	webview2 "github.com/jchv/go-webview2"

	"tvremote/internal/i18n"
)

//go:embed icon.ico
var trayIcon []byte

// runShell on Windows: a tray icon that sits silently in the notification area.
// Its menu has exactly two items: open the intro/QR window in WebView2, and
// quit. The mpv player window only appears when the user triggers
// playback from their phone.
func runShell(localURL string, httpSrv *http.Server) {
	desktopURL := localURL + "/desktop"

	onReady := func() {
		systray.SetIcon(trayIcon)
		systray.SetTitle("TV Remote MPV")
		systray.SetTooltip(i18n.System("tooltip"))

		mOpen := systray.AddMenuItem(i18n.System("open_main"), i18n.System("open_main_tip"))
		mLogs := systray.AddMenuItem(i18n.System("open_logs"), i18n.System("open_logs_tip"))
		systray.AddSeparator()
		mQuit := systray.AddMenuItem(i18n.System("quit"), i18n.System("quit_tip"))

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					openWindow(desktopURL)
				case <-mLogs.ClickedCh:
					if resp, err := http.Get(localURL + "/desktop/open-logs"); err == nil {
						resp.Body.Close()
					}
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()

		// Show the window once on first launch so users see the QR immediately.
		go openWindow(desktopURL)
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
			Title:  "TV Remote MPV",
			Width:  360,
			Height: 560,
			Center: true,
		},
	})
	if w == nil {
		return // WebView2 runtime missing
	}
	defer w.Destroy()
	w.Navigate(url)
	w.Run()
}
