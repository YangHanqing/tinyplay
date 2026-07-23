//go:build windows

package main

import (
	"context"
	"fmt"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"tvremote/internal/desktopinput"
)

const (
	inputMouse    = 0
	inputKeyboard = 1
	moveFlag      = 0x0001
	leftDownFlag  = 0x0002
	leftUpFlag    = 0x0004
	rightDownFlag = 0x0008
	rightUpFlag   = 0x0010
	wheelFlag     = 0x0800
	keyUpFlag     = 0x0002
	unicodeFlag   = 0x0004
)

type nativeMouseInput struct {
	dx, dy, mouseData, flags, time uint32
	extraInfo                      uintptr
}
type nativeKeyboardInput struct {
	vk, scan    uint16
	flags, time uint32
	extraInfo   uintptr
}
type nativeInput struct {
	kind  uint32
	_     uint32
	mouse nativeMouseInput
}

var sendInput = syscall.NewLazyDLL("user32.dll").NewProc("SendInput")

// startDesktopInputHost is intentionally process-global: SendInput targets the
// foreground Windows desktop, which is exactly the temporary set-top-box
// control model. It is not tied to the TinyPlay WebView.
func startDesktopInputHost() {
	desktopinput.Default.ReportState(desktopinput.Snapshot{Ready: true, PermissionGranted: true})
	go func() {
		var after uint64
		for {
			cmd, ok := desktopinput.Default.WaitCommand(context.Background(), after)
			if !ok {
				continue
			}
			after = cmd.ID
			if err := applyDesktopInput(cmd); err != nil {
				desktopinput.Default.ReportState(desktopinput.Snapshot{Ready: true, PermissionGranted: true, LastError: err.Error()})
			}
		}
	}()
}

func applyDesktopInput(cmd desktopinput.Command) error {
	switch cmd.Action {
	case desktopinput.ActionMove:
		return sendNative(mouseEvent(moveFlag, cmd.DX, cmd.DY, 0))
	case desktopinput.ActionLeftClick:
		return sendNative(mouseEvent(leftDownFlag, 0, 0, 0), mouseEvent(leftUpFlag, 0, 0, 0))
	case desktopinput.ActionRightClick:
		return sendNative(mouseEvent(rightDownFlag, 0, 0, 0), mouseEvent(rightUpFlag, 0, 0, 0))
	case desktopinput.ActionScroll:
		return sendNative(mouseEvent(wheelFlag, 0, 0, cmd.DY))
	case desktopinput.ActionKey:
		vk, ok := windowsVirtualKey(cmd.Text)
		if !ok {
			return fmt.Errorf("unsupported key")
		}
		return sendNative(keyEvent(vk, 0), keyEvent(vk, keyUpFlag))
	case desktopinput.ActionType:
		for _, unit := range utf16.Encode([]rune(cmd.Text)) {
			if err := sendNative(keyEvent(unit, unicodeFlag), keyEvent(unit, unicodeFlag|keyUpFlag)); err != nil {
				return err
			}
		}
	}
	return nil
}

func mouseEvent(flags uint32, dx, dy, data int) nativeInput {
	return nativeInput{kind: inputMouse, mouse: nativeMouseInput{dx: uint32(int32(dx)), dy: uint32(int32(dy)), mouseData: uint32(int32(data)), flags: flags}}
}
func keyEvent(code uint16, flags uint32) nativeInput {
	var out nativeInput
	out.kind = inputKeyboard
	k := (*nativeKeyboardInput)(unsafe.Pointer(&out.mouse))
	if flags&unicodeFlag != 0 {
		k.scan = code
	} else {
		k.vk = code
	}
	k.flags = flags
	return out
}
func sendNative(inputs ...nativeInput) error {
	if len(inputs) == 0 {
		return nil
	}
	n, _, err := sendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(nativeInput{}))
	if int(n) != len(inputs) {
		if err != syscall.Errno(0) {
			return err
		}
		return fmt.Errorf("Windows blocked desktop input")
	}
	return nil
}
func windowsVirtualKey(key string) (uint16, bool) {
	switch key {
	case "escape":
		return 0x1B, true
	case "enter":
		return 0x0D, true
	case "left":
		return 0x25, true
	case "up":
		return 0x26, true
	case "right":
		return 0x27, true
	case "down":
		return 0x28, true
	}
	return 0, false
}
