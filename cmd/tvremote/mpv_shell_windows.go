//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"tvremote/internal/i18n"
)

const mbIconWarning = 0x00000030

// messageBoxOK shows a native OK-only alert. Distinct from messageBoxYesNo
// (which is Yes/No, used elsewhere for "open the download page?" prompts):
// an invalid custom mpv path is just something to acknowledge, not a
// two-choice decision.
func messageBoxOK(title, text string) {
	_, _, _ = procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		uintptr(mbIconWarning),
	)
}

// mpvStatusResponse mirrors the JSON shape of GET/POST /desktop/mpv
// (internal/server/handlers_mpv.go). Kept minimal — only what the tray menu
// needs to render itself.
type mpvStatusResponse struct {
	Path             string `json:"path"`
	Source           string `json:"source"`
	Available        bool   `json:"available"`
	CustomConfigured bool   `json:"custom_configured"`
	CustomValid      bool   `json:"custom_valid"`
	BundledUnusable  bool   `json:"bundled_unusable"`
}

func fetchMPVStatus(coreURL string) (mpvStatusResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, coreURL+"/desktop/mpv", nil)
	if err != nil {
		return mpvStatusResponse{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return mpvStatusResponse{}, err
	}
	defer resp.Body.Close()
	var out mpvStatusResponse
	if resp.StatusCode != http.StatusOK {
		return mpvStatusResponse{}, fmt.Errorf("status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return mpvStatusResponse{}, err
	}
	return out, nil
}

// setCustomMPV posts a new (or, given "", cleared) custom mpv path to the
// core. On a validation failure the core's error detail comes back in the
// response body, which the caller surfaces via MessageBoxW.
func setCustomMPV(coreURL, path string) (mpvStatusResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"path": path})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, coreURL+"/desktop/mpv", bytes.NewReader(body))
	if err != nil {
		return mpvStatusResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return mpvStatusResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var detail struct {
			Detail string `json:"detail"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&detail)
		if detail.Detail == "" {
			detail.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return mpvStatusResponse{}, fmt.Errorf("%s", detail.Detail)
	}
	var out mpvStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return mpvStatusResponse{}, err
	}
	return out, nil
}

// mpvStatusTooltip renders the tray tooltip line for the current mpv state,
// mirroring the wording desktop.go's notices use so the two surfaces agree --
// including their order. A shipped runtime that cannot start outranks a stale
// custom path: with both true (a leftover custom path on a system too old for
// the bundled mpv) the stale-path wording would say the bundled player is in
// use, which is the exact false statement this state exists to prevent.
func mpvStatusTooltip(status mpvStatusResponse) string {
	switch {
	case status.BundledUnusable && status.Available:
		return fmt.Sprintf(i18n.System("mpv_too_new_tip"), status.Path)
	case status.BundledUnusable:
		return i18n.System("mpv_too_new_missing_tip")
	case status.CustomConfigured && !status.CustomValid:
		return i18n.System("mpv_custom_stale_tip")
	case status.Source == "custom":
		return fmt.Sprintf(i18n.System("mpv_custom_active_tip"), status.Path)
	default:
		return i18n.System("mpv_default_tip")
	}
}

// runChooseCustomMPV drives the whole "自定义 mpv 播放器…" action: show the
// native file picker, validate+persist via the core, and report failure with
// a native alert. hwnd may be 0 (no parent window is currently open) — the
// dialog and MessageBox both tolerate that.
func runChooseCustomMPV(hwnd uintptr, coreURL string, onDone func(mpvStatusResponse)) {
	path, ok := func() (string, bool) {
		// GetOpenFileNameW is modal and pumps a message loop on the calling
		// thread, so it gets the same treatment as every other Win32 UI path
		// in this shell (openWindow, the update dialog, toasts). The lock is
		// scoped to the dialog alone — the HTTP round-trip below has no
		// thread affinity and must not hold an OS thread hostage.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		return chooseMPVExecutable(hwnd, i18n.System("mpv_dialog_title"), i18n.System("mpv_custom_menu_tip"))
	}()
	if !ok {
		return
	}
	status, err := setCustomMPV(coreURL, path)
	if err != nil {
		messageBoxOK(i18n.System("mpv_invalid_title"), fmt.Sprintf(i18n.System("mpv_invalid_body"), err.Error()))
		return
	}
	if onDone != nil {
		onDone(status)
	}
}

// runRestoreDefaultMPV clears the persisted custom path.
func runRestoreDefaultMPV(coreURL string, onDone func(mpvStatusResponse)) {
	status, err := setCustomMPV(coreURL, "")
	if err != nil {
		messageBoxOK(i18n.System("mpv_invalid_title"), err.Error())
		return
	}
	if onDone != nil {
		onDone(status)
	}
}
