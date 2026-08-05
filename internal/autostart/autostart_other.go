//go:build !windows

package autostart

// Not implemented in Go outside Windows. On macOS the login item belongs to
// the app bundle and is registered by the AppKit shell through SMAppService
// (see macos/Sources/main.swift); the Go core runs as that bundle's child
// process and must not try to register itself as a separate login item.
const supported = false

func enabled() (bool, error) { return false, nil }

func setEnabled(bool) error { return nil }

func syncPath() error { return nil }
