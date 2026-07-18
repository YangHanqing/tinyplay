//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	webview2 "github.com/jchv/go-webview2"

	"tvremote/internal/config"
	"tvremote/internal/website"
)

// websiteHost owns the dedicated website WebView2 window and
// consumes loopback shell commands from the Go core. There is exactly one
// native window and one WebView2 page for website mode: selecting a catalog
// site navigates that page to the site's fixed home URL instead of creating a
// per-site window/WebView instance.
type websiteHost struct {
	coreURL string

	mu        sync.Mutex
	open      bool
	closing   bool
	pending   *websiteCmd
	windowCmd uint64
	lastCmd   uint64
	dispatch  chan websiteCmd
	quit      chan struct{}
}

type websiteCmd struct {
	ID     uint64
	Action string
	URL    string
	Text   string
	Label  string
}

type websiteActionResult struct {
	OK         bool     `json:"ok"`
	Status     string   `json:"status"`
	Error      string   `json:"error"`
	HintActive *bool    `json:"hint_active"`
	HintLabels []string `json:"labels"`
}

func startWebsiteShell(coreURL string) {
	// Injected script has no user gesture, so video.play() from the phone would
	// otherwise die on Chromium's autoplay policy. WebView2 reads this env var
	// when a browser environment is created; the flag is harmless for the QR
	// window's separate environment.
	const autoplayFlag = "--autoplay-policy=no-user-gesture-required"
	if args := os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS"); !strings.Contains(args, "--autoplay-policy") {
		if args != "" {
			args += " "
		}
		os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", args+autoplayFlag)
	}
	h := &websiteHost{
		// Shell transport is loopback-only; never use the LAN advertisement URL.
		coreURL:  loopbackCoreURL(coreURL),
		dispatch: make(chan websiteCmd, 8),
		quit:     make(chan struct{}),
	}
	go h.pollLoop()
}

func loopbackCoreURL(localURL string) string {
	u, err := url.Parse(localURL)
	if err != nil || u.Port() == "" {
		return "http://127.0.0.1:1980"
	}
	return "http://127.0.0.1:" + u.Port()
}

func (h *websiteHost) pollLoop() {
	client := &http.Client{Timeout: 35 * time.Second}
	for {
		select {
		case <-h.quit:
			return
		default:
		}
		url := fmt.Sprintf("%s/desktop/website/poll?after=%d", h.coreURL, h.lastCmd)
		resp, err := client.Get(url)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			time.Sleep(time.Second)
			continue
		}
		var payload struct {
			OK      bool `json:"ok"`
			Empty   bool `json:"empty"`
			Command *struct {
				ID     uint64 `json:"id"`
				Action string `json:"action"`
				URL    string `json:"url"`
				Text   string `json:"text"`
				Label  string `json:"label"`
			} `json:"command"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.Empty || payload.Command == nil {
			continue
		}
		cmd := websiteCmd{
			ID:     payload.Command.ID,
			Action: payload.Command.Action,
			URL:    payload.Command.URL,
			Text:   payload.Command.Text,
			Label:  payload.Command.Label,
		}
		h.lastCmd = cmd.ID
		h.handleCommand(cmd)
	}
}

func (h *websiteHost) handleCommand(cmd websiteCmd) {
	switch cmd.Action {
	case website.ActionOpen:
		h.ensureWindow(cmd)
	case website.ActionClose:
		h.closeWindow()
		h.report(map[string]any{
			"open":       false,
			"status":     "closed",
			"action":     website.ActionClose,
			"command_id": cmd.ID,
		})
	default:
		h.mu.Lock()
		open := h.open
		h.mu.Unlock()
		if !open {
			h.report(map[string]any{
				"status":     "error",
				"error":      "window_not_open",
				"action":     cmd.Action,
				"command_id": cmd.ID,
			})
			return
		}
		// Deliver to the window thread via channel; result reported from there.
		select {
		case h.dispatch <- cmd:
		default:
			// Drop if the window is busy; report bounded failure.
			h.report(map[string]any{
				"status":     "error",
				"error":      "busy",
				"action":     cmd.Action,
				"command_id": cmd.ID,
			})
		}
	}
}

func (h *websiteHost) ensureWindow(cmd websiteCmd) {
	h.mu.Lock()
	if h.closing {
		// A close is already queued on the old UI thread. Reopen only after its
		// message loop exits so rapid Close→Open cannot lose the new request.
		pending := cmd
		h.pending = &pending
		h.mu.Unlock()
		return
	}
	if h.open {
		// Reuse the singleton page. RequestOpen always carries the catalog's
		// fixed home URL, so Bilibili -> catalog -> Youku lands on Youku's home,
		// and selecting Bilibili again lands on Bilibili's home. Keep the latest
		// lifecycle id so a later native-close report refers to this open.
		h.windowCmd = cmd.ID
		dispatch := h.dispatch
		h.mu.Unlock()
		select {
		case dispatch <- cmd:
		default:
			h.report(map[string]any{
				"status":     "error",
				"error":      "busy",
				"action":     website.ActionOpen,
				"command_id": cmd.ID,
			})
		}
		return
	}
	h.open = true
	h.windowCmd = cmd.ID
	// Fresh dispatch channel per window lifetime.
	h.dispatch = make(chan websiteCmd, 8)
	dispatch := h.dispatch
	h.mu.Unlock()

	go h.runWindow(cmd, dispatch)
}

func (h *websiteHost) closeWindow() {
	h.mu.Lock()
	if !h.open || h.closing {
		h.mu.Unlock()
		return
	}
	h.closing = true
	select {
	case h.dispatch <- websiteCmd{Action: website.ActionClose}:
	default:
	}
	h.mu.Unlock()
}

func (h *websiteHost) runWindow(initial websiteCmd, dispatch <-chan websiteCmd) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	dataPath := filepath.Join(config.DataDir(), "webview2-website")
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:    false,
		DataPath: dataPath,
		WindowOptions: webview2.WindowOptions{
			Title:  "TinyPlay Website",
			Width:  1280,
			Height: 720,
			Center: true,
		},
	})
	if w == nil {
		h.mu.Lock()
		h.open = false
		h.windowCmd = 0
		h.mu.Unlock()
		h.report(map[string]any{
			"open":       false,
			"status":     "error",
			"error":      "webview2_unavailable",
			"action":     website.ActionOpen,
			"command_id": initial.ID,
		})
		return
	}
	done := make(chan struct{})
	defer func() {
		close(done)
		w.Destroy()
		h.mu.Lock()
		h.open = false
		h.closing = false
		pending := h.pending
		h.pending = nil
		closedWindowCmd := h.windowCmd
		h.windowCmd = 0
		h.mu.Unlock()
		if pending != nil {
			h.ensureWindow(*pending)
		} else {
			// Alt-F4 / native close ends the message loop and lands here.
			h.report(map[string]any{
				"open":       false,
				"status":     "closed",
				"action":     "window_closed",
				"command_id": closedWindowCmd,
			})
		}
	}()

	// Report every top-level document URL (including cross-origin loads). Init
	// scripts re-run on each new document, so this covers navigations that the
	// high-level WebView API does not expose callbacks for.
	_ = w.Bind("tinyplayWebsiteNav", func(href string) error {
		href = string(href)
		if href == "" {
			return nil
		}
		go h.report(map[string]any{
			"open":        true,
			"status":      "navigated",
			"action":      "navigation",
			"current_url": href,
		})
		return nil
	})

	navReporter := `(function(){try{if(window.top!==window)return;var h=String(location.href||'');if(h&&window.tinyplayWebsiteNav){window.tinyplayWebsiteNav(h);}}catch(e){}})();`
	// Inject nav reporter + shared DOM controller before every document load.
	w.Init(navReporter + "\n" + website.ControllerJS)
	_ = w.Bind("tinyplayWebsiteReport", func(result websiteActionResult, commandID uint64, action string) error {
		body := map[string]any{
			"open":       true,
			"status":     result.Status,
			"action":     action,
			"command_id": commandID,
		}
		if body["status"] == "" {
			if result.OK {
				body["status"] = "ok"
			} else {
				body["status"] = "error"
			}
		}
		if result.Error != "" {
			body["error"] = result.Error
		}
		if result.HintActive != nil {
			body["hint_active"] = *result.HintActive
		}
		if result.HintLabels != nil {
			body["hint_labels"] = result.HintLabels
		}
		go h.report(body)
		return nil
	})

	// WebView2 deliberately does not resize its host HWND when a page enters
	// the DOM Fullscreen API. Edge owns its top-level window and does that work
	// itself; an embedded WebView2 expects the app to react to the fullscreen
	// state. ControllerJS reports fullscreenchange from every document/frame,
	// and this bridge promotes the one website HWND to true monitor fullscreen.
	// Exiting (including Esc) restores the normal maximized browsing window.
	var fs winFullscreen
	_ = w.Bind("tinyplayWebsiteSetFullscreen", func(enter bool) error {
		hwnd := uintptr(w.Window())
		if hwnd == 0 {
			return nil
		}
		if enter {
			return fs.enter(hwnd)
		}
		fs.exit(hwnd)
		maximizeWindow(hwnd)
		return nil
	})

	// A normal maximized app window, not a borderless kiosk surface: it keeps the
	// system title bar (close / maximize) and the taskbar, so the user can always
	// close it natively — the message loop's WM_CLOSE path then destroys it
	// cleanly and the deferred cleanup reports window_closed to the broker. A
	// site's ordinary page remains here. Only an actual DOM fullscreen
	// transition temporarily promotes this host window to monitor fullscreen.
	hwnd := uintptr(w.Window())
	if hwnd != 0 {
		maximizeWindow(hwnd)
	}

	// Pump shell commands while the window message loop runs.
	go func() {
		for {
			select {
			case <-done:
				return
			case cmd, ok := <-dispatch:
				if !ok {
					return
				}
				c := cmd
				w.Dispatch(func() {
					h.applyOnWindow(w, c)
				})
			}
		}
	}()

	// Only report open after WebView2 actually created the native window.
	h.report(map[string]any{
		"open":       true,
		"status":     "open",
		"action":     website.ActionOpen,
		"command_id": initial.ID,
	})
	if initial.URL != "" {
		w.Navigate(initial.URL)
	}
	// w.Run blocks until the native window closes.
	w.Run()
}

func (h *websiteHost) applyOnWindow(w webview2.WebView, cmd websiteCmd) {
	switch cmd.Action {
	case website.ActionClose:
		// Destroy, not Terminate. Terminate only exits w.Run's message loop; it
		// does not destroy the native HWND. Destroy posts WM_CLOSE while this loop
		// is still pumping, so phone-initiated close follows the same real teardown
		// path as the title-bar X / Alt-F4.
		w.Destroy()
	case website.ActionOpen:
		if cmd.URL == "" {
			h.report(map[string]any{"status": "error", "error": "unknown_site", "action": website.ActionOpen, "command_id": cmd.ID, "open": true})
			return
		}
		// Best-effort pause before replacing the current document. Navigate on
		// this same WebView2 instance; do not create a site-specific page.
		w.Eval(`document.querySelectorAll('video,audio').forEach(function(m){try{m.pause();m.muted=true;}catch(e){}})`)
		w.Navigate(cmd.URL)
		h.report(map[string]any{"status": "open", "action": website.ActionOpen, "command_id": cmd.ID, "open": true})
	case website.ActionBack:
		w.Eval(`history.back()`)
		// Location will be reported by the next document's Init nav reporter.
		h.report(map[string]any{"status": "back", "action": website.ActionBack, "command_id": cmd.ID, "open": true})
	case website.ActionForward:
		w.Eval(`history.forward()`)
		h.report(map[string]any{"status": "forward", "action": website.ActionForward, "command_id": cmd.ID, "open": true})
	case website.ActionHome:
		if cmd.URL == "" {
			h.report(map[string]any{"status": "error", "error": "home_unavailable", "action": website.ActionHome, "command_id": cmd.ID, "open": true})
			return
		}
		w.Navigate(cmd.URL)
		h.report(map[string]any{"status": "home", "action": website.ActionHome, "command_id": cmd.ID, "open": true})
	case website.ActionLogin:
		if cmd.URL == "" {
			h.report(map[string]any{"status": "error", "error": "login_unavailable", "action": website.ActionLogin, "command_id": cmd.ID, "open": true})
			return
		}
		// Broker-supplied fixed login route only; never phone-provided URL text.
		w.Navigate(cmd.URL)
		h.report(map[string]any{"status": "login", "action": website.ActionLogin, "command_id": cmd.ID, "open": true})
	case website.ActionRefresh:
		w.Eval(`window.location.reload()`)
		h.report(map[string]any{"status": "refresh", "action": website.ActionRefresh, "command_id": cmd.ID, "open": true})
	default:
		// DOM actions via shared controller.
		payload, _ := json.Marshal(map[string]any{
			"action": cmd.Action,
			"text":   cmd.Text,
			"label":  cmd.Label,
		})
		// WebView2's Eval is fire-and-forget, so send the real controller result
		// through the narrow typed RPC binding above instead of guessing success.
		// handle() may return a Promise (oracle-checked transport actions), so
		// resolve before reporting — the shell must report the confirmed effect.
		js := fmt.Sprintf(`(function(){function send(r){try{if(window.tinyplayWebsiteReport){window.tinyplayWebsiteReport(r||{ok:false,status:'error',error:'exception'},%d,%q);}}catch(e){}}var r;try{r=window.__tinyplayWebsite?window.__tinyplayWebsite.handle(%s):{ok:false,status:'error',error:'no_controller'};}catch(e){send({ok:false,status:'error',error:'exception'});return;}if(r&&typeof r.then==='function'){r.then(send,function(){send({ok:false,status:'error',error:'exception'});});}else{send(r);}})()`, cmd.ID, cmd.Action, string(payload))
		w.Eval(js)
	}
}

func (h *websiteHost) report(body map[string]any) {
	raw, err := json.Marshal(body)
	if err != nil {
		return
	}
	req, err := http.NewRequest(http.MethodPost, h.coreURL+"/desktop/website/report", bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("website report failed: %v", err)
		return
	}
	resp.Body.Close()
}
