//go:build !windows

package filesource

// osRootBase resolves where to start browsing a local/nfs source that has no
// configured RootPath. Unix has a single root ("/"), so browsing starts
// there immediately — no synthetic "pick a root" step, matching how
// SMB/WebDAV browsing at path="" shows the configured root's contents
// directly.
func osRootBase(segs []string) (base string, rest []string, drivePicker bool) {
	return "/", segs, false
}

// driveEntries is never called on Unix (osRootBase never signals
// drivePicker), but must exist so the package builds on every platform.
func driveEntries() []Entry { return nil }
