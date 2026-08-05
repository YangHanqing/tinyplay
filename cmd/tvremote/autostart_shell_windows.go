//go:build windows

package main

import (
	"log"
	"os"

	"tvremote/internal/autostart"

	"fyne.io/systray"

	"tvremote/internal/i18n"
)

// ── Advanced settings: start at login ──
//
// Unlike the custom mpv path, this preference is not stored in config.json and
// has no core route: the Windows startup list itself is the state (see
// internal/autostart). The tray owns it because it is a property of this
// installed copy of TinyPlay, not of the media setup the phone configures.

// launchedAtLogin reports whether Windows started this process from the Run
// key rather than the user double-clicking the app. The QR window is opened on
// an ordinary launch because that is what someone who just started TinyPlay
// wants to see; doing it on every sign-in would put a window in front of
// whatever they were actually there to do.
func launchedAtLogin() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--autostart" {
			return true
		}
	}
	return false
}

// readAutostart reads the current state for the tray checkbox. A read failure
// is reported as "off" — the same thing the user sees for a genuinely absent
// entry, and clicking the item then simply tries to write it.
func readAutostart() bool {
	on, err := autostart.Enabled()
	if err != nil {
		log.Printf("autostart: read failed: %v", err)
		return false
	}
	return on
}

// toggleAutostart flips the startup entry and re-syncs the menu checkbox from
// what the OS reports afterwards, rather than from what we asked for, so a
// failed write leaves the checkbox telling the truth.
func toggleAutostart(item *systray.MenuItem) {
	target := !readAutostart()
	if err := autostart.SetEnabled(target); err != nil {
		log.Printf("autostart: set %v failed: %v", target, err)
		messageBoxOK(i18n.System("autostart_failed_title"), i18n.System("autostart_failed_body", err.Error()))
	}
	applyAutostartCheckbox(item)
}

func applyAutostartCheckbox(item *systray.MenuItem) {
	if readAutostart() {
		item.Check()
	} else {
		item.Uncheck()
	}
}
