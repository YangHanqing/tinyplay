//go:build windows

package autostart

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const supported = true

// runKeyPath is the per-user startup list. HKCU rather than HKLM on purpose:
// TinyPlay installs per-user (see windows/TinyPlay.iss, PrivilegesRequired=lowest),
// so writing a machine-wide entry would both need elevation and start the app
// for accounts that never installed it.
const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// runValueName is the value under that key. It doubles as the label Windows
// shows in Task Manager's Startup tab, so it stays the product name. A var,
// not a const, so the test can use a throwaway name instead of touching the
// real user's startup list.
var runValueName = "TinyPlay"

// autostartFlag marks a login-triggered launch so the shell can stay quietly
// in the tray instead of opening the QR window on every sign-in. See
// launchedAtLogin in cmd/tvremote/shell_windows.go.
const autostartFlag = "--autostart"

// startupCommand is what gets written to the Run value: the absolute path to
// this executable, quoted (install paths contain spaces — "Program Files",
// and per-user installs land under a folder named after the account), plus
// the flag that tells the shell it was started by the system.
func startupCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	return `"` + exe + `" ` + autostartFlag, nil
}

// readValue returns the current Run value, or "" when autostart is off. A
// missing key and a missing value are both simply "off" — the Run key does not
// exist at all on a fresh account until something registers.
func readValue() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	defer k.Close()
	value, _, err := k.GetStringValue(runValueName)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func enabled() (bool, error) {
	value, err := readValue()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(value) != "", nil
}

func setEnabled(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	command, err := startupCommand()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValueName, command)
}

func syncPath() error {
	current, err := readValue()
	if err != nil || strings.TrimSpace(current) == "" {
		return err
	}
	want, err := startupCommand()
	if err != nil {
		return err
	}
	if current == want {
		return nil
	}
	return setEnabled(true)
}
