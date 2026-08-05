// Package autostart toggles "start TinyPlay when this computer starts".
//
// This is a desktop-shell preference, not a phone-facing one: it is reachable
// only from the tray / menu-bar "高级设置" submenu, next to the custom mpv
// player entry, and it is off until the user turns it on.
//
// The operating system — not config.json — is the source of truth. On Windows
// that is the per-user HKCU Run key; on macOS the menu-bar shell owns the
// equivalent through SMAppService (a login item must be registered by the app
// bundle itself, which the Go core, a child process, cannot do), so this
// package reports itself unsupported there. Keeping the OS authoritative means
// a value the user removed by hand — in Task Manager's Startup tab, or in
// System Settings → Login Items — is never silently resurrected by a stale
// copy of the flag on disk.
package autostart

// Supported reports whether this platform has an implementation. Callers must
// hide the menu entry entirely when it is false rather than show a control
// that cannot do anything.
func Supported() bool { return supported }

// Enabled reports whether TinyPlay is currently registered to start at login.
func Enabled() (bool, error) { return enabled() }

// SetEnabled registers or unregisters TinyPlay for login start. Enabling
// always (re)writes the current executable path, so calling it again after a
// move or an upgrade repairs a stale entry.
func SetEnabled(on bool) error { return setEnabled(on) }

// Sync repairs an entry that points at an executable which has since moved —
// an in-place upgrade into a different directory, or a portable copy relocated
// by hand. It is a no-op when autostart is off, and best-effort: a failure to
// repair must never keep the app from starting, so callers ignore the error.
func Sync() error { return syncPath() }
