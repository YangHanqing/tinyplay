//go:build !windows

package filesource

import "os"

// osRootBase resolves where to start browsing a local/nfs source that has no
// configured RootPath. Unix has a single root ("/"), so browsing starts
// there immediately — no synthetic "pick a root" step, matching how
// SMB/WebDAV browsing at path="" shows the configured root's contents
// directly.
func osRootBase(segs []string) (base string, rest []string, drivePicker bool) {
	return "/", segs, false
}

// osNormalizeConfiguredRoot is a no-op outside Windows.
func osNormalizeConfiguredRoot(root string) string { return root }

// osFilesystemPath is a no-op outside Windows — Unix path APIs have no
// MAX_PATH extended-prefix requirement.
func osFilesystemPath(path string) string { return path }

// driveEntries is never called on Unix (osRootBase never signals
// drivePicker), but must exist so the package builds on every platform.
func driveEntries() []Entry { return nil }

func localListError(full string, err error) error {
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return errf(404, "Folder not found")
	}
	return errf(502, "Could not list folder: %v", err)
}
