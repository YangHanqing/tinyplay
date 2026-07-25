package filesource

import "strings"

// formatWindowsFilesystemPath rewrites a Windows path so Win32 file APIs accept
// deep trees and names that Explorer can open but the classic MAX_PATH surface
// cannot (long NAS paths on a mapped drive, trailing dots/spaces from a Linux
// NFS export, some reparse-point layouts).
//
// Implemented with pure string ops (not path/filepath) so the same logic is
// correct when unit-tested on non-Windows hosts. The logical path used in API
// responses stays unprefixed; only OS open/list calls should pass through this
// helper.
func formatWindowsFilesystemPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	path = strings.ReplaceAll(path, `/`, `\`)

	// Already extended or device path — leave untouched.
	if strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\\.`) {
		return path
	}

	unc := false
	if strings.HasPrefix(path, `\\`) {
		unc = true
		path = path[2:]
	}

	parts := strings.Split(path, `\`)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		if p == ".." {
			if len(out) == 0 {
				continue
			}
			// Do not pop a drive letter volume ("Z:").
			if len(out) == 1 && isWindowsVolume(out[0]) {
				continue
			}
			out = out[:len(out)-1]
			continue
		}
		out = append(out, p)
	}
	path = strings.Join(out, `\`)
	if unc {
		if path == "" {
			return `\\`
		}
		return `\\?\UNC\` + path
	}

	// Drive-only "Z:" → "Z:\" then extended.
	if isWindowsVolume(path) {
		return `\\?\` + path + `\`
	}
	// Absolute drive path "Z:\Media\..."
	if len(path) >= 3 && isDriveLetter(path[0]) && path[1] == ':' {
		if path[2] != '\\' {
			path = path[:2] + `\` + path[2:]
		}
		return `\\?\` + path
	}
	return path
}

// normalizeWindowsConfiguredRoot makes a saved/local-picker root usable with
// filepath.IsAbs / Abs. The important case is a drive-only root "Z:" / "Z:/",
// which Go treats as drive-relative rather than "Z:\".
func normalizeWindowsConfiguredRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	cleaned := strings.ReplaceAll(root, `/`, `\`)
	cleaned = strings.TrimRight(cleaned, `\`)
	if isWindowsVolume(cleaned) {
		return string([]byte{toUpperDrive(cleaned[0])}) + `:\`
	}
	// Preserve the caller's separators for non-drive-only roots; filepath on
	// Windows accepts both.
	if strings.Contains(root, `/`) {
		return root
	}
	return cleaned
}

func isWindowsVolume(s string) bool {
	return len(s) == 2 && isDriveLetter(s[0]) && s[1] == ':'
}

func isDriveLetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func toUpperDrive(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 32
	}
	return b
}
