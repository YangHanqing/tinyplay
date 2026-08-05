//go:build windows

package main

import (
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// comdlg32's classic Open dialog, chosen deliberately over anything
// WebView2-adjacent: systray has no dialog primitive of its own, and an
// `<input type=file>` inside the WebView2 QR window only ever hands JS a
// browser-sandboxed File object, never the real filesystem path an
// exec.Command needs. GetOpenFileNameW is the one Win32 call that gives us
// that path directly, filtered to executables, with the user free to
// navigate anywhere (a hand-installed mpv under Program Files, a portable
// build under an arbitrary directory, etc.).
var (
	comdlg32             = windows.NewLazySystemDLL("comdlg32.dll")
	procGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
)

// openFileNameW mirrors OPENFILENAMEW (commdlg.h). Field order and widths
// must match the real struct exactly: comdlg32 reads/writes this memory
// directly, so any drift here corrupts the call rather than failing loudly.
type openFileNameW struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

const (
	ofnFileMustExist = 0x00001000
	ofnPathMustExist = 0x00000800
	ofnHideReadonly  = 0x00000004
	ofnNoChangeDir   = 0x00000008

	maxFilePathBuffer = 32768 // matches Windows' own long-path ceiling
)

// chooseMPVExecutable shows a native "choose a file" dialog filtered to
// executables and returns the selected path. ok is false if the dialog
// failed to open or the user cancelled — both are silent no-ops upstream,
// matching how every other cancel-able action in this shell behaves.
func chooseMPVExecutable(owner uintptr, title, filterLabel string) (path string, ok bool) {
	buf := make([]uint16, maxFilePathBuffer)
	filter := buildExecutableFilter(filterLabel)
	titlePtr, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return "", false
	}
	ofn := openFileNameW{
		hwndOwner:   owner,
		lpstrFilter: &filter[0],
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  titlePtr,
		flags:       ofnFileMustExist | ofnPathMustExist | ofnHideReadonly | ofnNoChangeDir,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))

	ret, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ret == 0 {
		return "", false
	}
	return windows.UTF16ToString(buf), true
}

// buildExecutableFilter encodes comdlg32's filter format: NUL-separated
// "label","pattern" pairs, double-NUL terminated. windows.UTF16FromString
// can't be used for this — it rejects embedded NULs — so the buffer is built
// by hand with unicode/utf16.
func buildExecutableFilter(exeLabel string) []uint16 {
	label := utf16.Encode([]rune(exeLabel))
	pattern := utf16.Encode([]rune("*.exe"))
	out := make([]uint16, 0, len(label)+len(pattern)+3)
	out = append(out, label...)
	out = append(out, 0)
	out = append(out, pattern...)
	out = append(out, 0, 0)
	return out
}
