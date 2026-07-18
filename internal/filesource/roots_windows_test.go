//go:build windows

package filesource

import (
	"path/filepath"
	"testing"
)

func TestDriveEntriesUseAbsoluteRootPaths(t *testing.T) {
	for _, entry := range driveEntries() {
		if !filepath.IsAbs(entry.Path) {
			t.Fatalf("drive picker path %q must be absolute", entry.Path)
		}
	}
}
