//go:build windows

package filesource

import (
	"os"
	"strings"
)

// osRootBase resolves where to start browsing a local/nfs source that has no
// configured RootPath. Windows has no single root — at depth 0 the picker
// must choose a drive first (signaled by drivePicker), then subsequent
// segments resolve against "<drive>:\".
func osRootBase(segs []string) (base string, rest []string, drivePicker bool) {
	if len(segs) == 0 {
		return "", nil, true
	}
	drive := segs[0]
	// Picker paths are "Z:/Media/..." → first segment "Z:". Require the
	// drive-letter form so a coincidental folder named "Z" is not treated as
	// a volume root.
	if len(drive) == 2 && isDriveLetter(drive[0]) && drive[1] == ':' {
		return string([]byte{toUpperDrive(drive[0])}) + `\`, segs[1:], false
	}
	return drive + `\`, segs[1:], false
}

// osNormalizeConfiguredRoot is applied to Server.RootPath before IsAbs/Abs.
func osNormalizeConfiguredRoot(root string) string {
	return normalizeWindowsConfiguredRoot(root)
}

// osFilesystemPath adds the Win32 extended-length prefix when needed.
func osFilesystemPath(path string) string {
	return formatWindowsFilesystemPath(path)
}

// driveEntries lists the drive letters that actually exist on this machine,
// presented as directory entries so the folder picker can browse into one
// exactly like any other folder.
func driveEntries() []Entry {
	out := []Entry{}
	for c := byte('A'); c <= 'Z'; c++ {
		letter := string(c)
		// Stat through the extended path form so a long-path-only volume is
		// still discoverable the same way later list/open calls work.
		if _, err := os.Stat(osFilesystemPath(letter + `:\`)); err != nil {
			// Fall back to the classic form — some virtual drives reject \\?\
			// on the bare root while still listing children normally.
			if _, err2 := os.Stat(letter + `:\`); err2 != nil {
				continue
			}
		}
		// Keep the trailing slash in the API path. "C:" is drive-relative on
		// Windows, while "C:/" is the absolute drive root users selected.
		out = append(out, Entry{Name: letter + ":", Path: letter + ":/", IsDir: true})
	}
	return out
}

// localListError turns a Windows directory-open failure into a stable API
// error. Network mapped drives often surface deep/long paths as
// ERROR_PATH_NOT_FOUND (os.IsNotExist) even when Explorer can open them;
// keep the OS text so the phone UI is actionable.
func localListError(full string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// Strip any extended-length prefix from the path we echo back.
	display := strings.TrimPrefix(strings.TrimPrefix(full, `\\?\UNC\`), `\\?\`)
	if os.IsNotExist(err) {
		return errf(404, "Folder not found (%s): %s", display, msg)
	}
	return errf(502, "Could not list folder (%s): %s", display, msg)
}
