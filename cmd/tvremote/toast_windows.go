//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"tvremote/internal/i18n"
)

// toastHost drives a small, always-on-top, non-activating text indicator so a
// slow remote source (IPTV, remote Emby/Jellyfin/Plex) doesn't leave the
// monitor blank or frozen with no sign the phone's tap registered — mpv
// itself does not create its window until it has probed enough of the stream
// to know the video format, which can take several seconds on a slow source.
//
// Mirrors the macOS ToastPanelController: same long-poll against
// GET /api/player/state, same two modes ("connecting" while a Play() attempt
// has not yet produced a frame, "autoplay" while host autoplay's next-episode
// grace period is counting down — see desktopToastState in the Go core).
// Independent of mpv's own window so it covers both cases the user can be
// in: nothing on screen yet (cold start), or mpv already full-screen on a
// previous title (autoplay countdown, or switching titles).
//
// Deliberately a single stock "STATIC" popup window driven by raw Win32
// calls, not WebView2: it must appear in milliseconds and must not itself
// become the next thing the user is waiting on. WS_EX_TOPMOST is enough to
// float over mpv's borderless-fullscreen window — Windows has no equivalent
// of macOS Spaces to fight here.
type toastHost struct {
	coreURL string

	mu      sync.Mutex
	hwnd    uintptr
	loopTID uint32

	pendingMode      string
	pendingRemaining int
}

func startToastShell(coreURL string) {
	h := &toastHost{coreURL: loopbackCoreURL(coreURL)}
	go h.runWindowThread()
	go h.pollLoop()
}

// pollLoop mirrors websiteHost.pollLoop's shape but talks to the ordinary
// player-state long-poll (already used by the phone UI and by the macOS
// shell's standby monitor) instead of a dedicated command queue: the toast
// has nothing to report back, it only ever reacts to state.
func (h *toastHost) pollLoop() {
	client := &http.Client{Timeout: 35 * time.Second}
	var after uint64
	haveAfter := false
	for {
		u := h.coreURL + "/api/player/state"
		if haveAfter {
			u += fmt.Sprintf("?after_revision=%d", after)
		}
		resp, err := client.Get(u)
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
			PlaybackRevision  uint64 `json:"playback_revision"`
			DesktopToastMode  string `json:"desktop_toast_mode"`
			AutoplayRemaining *int64 `json:"autoplay_remaining_ms"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			time.Sleep(time.Second)
			continue
		}
		after = payload.PlaybackRevision
		haveAfter = true
		remaining := 0
		if payload.AutoplayRemaining != nil {
			// Ceil to seconds so the countdown never visibly shows 0 while a
			// fraction of a second of the grace period actually remains.
			remaining = int((*payload.AutoplayRemaining + 999) / 1000)
		}
		h.apply(payload.DesktopToastMode, remaining)
	}
}

// apply hands new state to the window thread. HWND operations must run on
// the thread that created the window, so this only records the pending state
// and wakes that thread's message loop via a custom WM_APP message; the loop
// itself calls applyPending to do the actual SetWindowText/show/hide.
func (h *toastHost) apply(mode string, remainingSeconds int) {
	h.mu.Lock()
	h.pendingMode = mode
	h.pendingRemaining = remainingSeconds
	tid := h.loopTID
	h.mu.Unlock()
	if tid == 0 {
		// Window thread hasn't finished creating the HWND yet; the next
		// long-poll response (or the natural revision bump that follows any
		// real state change) will retry.
		return
	}
	_, _, _ = procPostThreadMessageW.Call(uintptr(tid), uintptr(wmToastApply), 0, 0)
}

func (h *toastHost) runWindowThread() {
	runtime.LockOSThread()

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className, err := syscall.UTF16PtrFromString("STATIC")
	if err != nil {
		return
	}
	windowName, err := syscall.UTF16PtrFromString("")
	if err != nil {
		return
	}
	exStyle := uintptr(wsExLayered | wsExTopmost | wsExToolWindow | wsExNoActivate)
	style := uintptr(wsPopup | ssCenter | ssCenterImage)
	hwnd, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		style,
		0, 0, uintptr(toastWidth), uintptr(toastHeight),
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return
	}
	font, _, _ := procGetStockObject.Call(defaultGUIFont)
	_, _, _ = procSendMessageW.Call(hwnd, wmSetFont, font, 1)
	// LWA_ALPHA applies uniformly to the whole window (background + text);
	// a translucent pill with fully-opaque text would need custom painting,
	// which this deliberately plain indicator skips.
	_, _, _ = procSetLayeredWindowAttributes.Call(hwnd, 0, uintptr(toastAlpha), lwaAlpha)

	h.mu.Lock()
	h.hwnd = hwnd
	h.loopTID = getCurrentThreadID()
	h.mu.Unlock()

	var msg win32Msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
		if msg.Message == wmToastApply {
			h.applyPending()
			continue
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (h *toastHost) applyPending() {
	h.mu.Lock()
	mode := h.pendingMode
	remaining := h.pendingRemaining
	hwnd := h.hwnd
	h.mu.Unlock()
	if hwnd == 0 {
		return
	}
	if mode == "" {
		_, _, _ = procShowWindow.Call(hwnd, uintptr(swHide))
		return
	}
	var text string
	switch mode {
	case "autoplay":
		if remaining < 0 {
			remaining = 0
		}
		text = i18n.System("toast_autoplay_countdown", remaining)
	default:
		text = i18n.System("toast_connecting")
	}
	textPtr, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	_, _, _ = procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(textPtr)))

	x, y := toastPosition(hwnd)
	_, _, _ = procSetWindowPos.Call(hwnd, hwndTopmost,
		uintptr(x), uintptr(y), uintptr(toastWidth), uintptr(toastHeight),
		uintptr(swpNoActivate|swpShowWindow))
}

// toastPosition centers the toast near the bottom of whichever monitor mpv
// would appear on (same "nearest to current window" heuristic winFullscreen
// already uses for the QR/website windows), clear of a Dock/taskbar left in
// an auto-hide sliver state.
func toastPosition(hwnd uintptr) (x, y int) {
	x, y = 40, 40 // degenerate fallback if monitor lookup fails
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	if mon == 0 {
		return
	}
	var mi winMonitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	if ok == 0 {
		return
	}
	screenWidth := int(mi.Monitor.Right - mi.Monitor.Left)
	x = int(mi.Monitor.Left) + (screenWidth-toastWidth)/2
	y = int(mi.Monitor.Bottom) - toastHeight - 96
	return
}

// win32Msg mirrors the Win32 MSG struct layout on amd64 (this project only
// ships a windows/amd64 build): a uint32 field is followed by 4 bytes of
// padding so WParam lands on its natural 8-byte boundary.
type win32Msg struct {
	Hwnd    uintptr
	Message uint32
	_       uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
}

const (
	toastWidth  = 300
	toastHeight = 42

	wmApp        = 0x8000
	wmToastApply = wmApp + 1
	wmSetFont    = 0x0030

	wsExLayered    = 0x00080000
	wsExTopmost    = 0x00000008
	wsExToolWindow = 0x00000080
	wsExNoActivate = 0x08000000

	ssCenter      = 0x00000001
	ssCenterImage = 0x00000200

	lwaAlpha   = 0x2
	toastAlpha = 235 // out of 255; slightly translucent

	defaultGUIFont = 17 // GetStockObject(DEFAULT_GUI_FONT)
	swHide         = 0  // SW_HIDE
)

// hwndTopmost is HWND_TOPMOST (-1 as an unsigned window handle), which keeps
// the toast above every other top-level window including mpv's own
// borderless-fullscreen one. hwndTop (0, used elsewhere in this package for
// the website window) only reorders within the non-topmost band, which is
// not enough here.
var hwndTopmost = ^uintptr(0)

func getCurrentThreadID() uint32 {
	ret, _, _ := procGetCurrentThreadId.Call()
	return uint32(ret)
}

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadId         = kernel32.NewProc("GetCurrentThreadId")
	procGetStockObject             = gdi32.NewProc("GetStockObject")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procSetWindowTextW             = user32.NewProc("SetWindowTextW")
	procSendMessageW               = user32.NewProc("SendMessageW")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW         = user32.NewProc("PostThreadMessageW")
)
