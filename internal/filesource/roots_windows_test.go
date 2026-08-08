//go:build windows

package filesource

import (
	"path/filepath"
	"strings"
	"testing"

	"tvremote/internal/config"
)

func TestDriveEntriesUseAbsoluteRootPaths(t *testing.T) {
	for _, entry := range driveEntries() {
		if !filepath.IsAbs(entry.Path) {
			t.Fatalf("drive picker path %q must be absolute", entry.Path)
		}
	}
}

// The previous test checked only what driveEntries hands the phone; the
// regression was in what osRootBase makes of that value when the phone hands
// it back, which nothing covered.
func TestOSRootBaseResolvesDriveSegmentToVolumeRoot(t *testing.T) {
	base, rest, drivePicker := osRootBase([]string{"C:", "Users"})
	if drivePicker {
		t.Fatalf("a path that already names a drive must not re-open the picker")
	}
	if base != `C:\` {
		t.Fatalf("osRootBase base = %q, want %q", base, `C:\`)
	}
	if !filepath.IsAbs(base) {
		t.Fatalf("base %q must be absolute, or filepath.Rel cannot relate it to a request path", base)
	}
	if len(rest) != 1 || rest[0] != "Users" {
		t.Fatalf("rest = %v, want [Users]", rest)
	}
}

// End-to-end over the real Windows filepath semantics: clicking a drive in
// the picker must resolve to that drive's root, not fail as a traversal.
func TestLocalBrowseIntoDriveIsNotTraversal(t *testing.T) {
	c := New(&config.Server{Name: "Local", Type: "local"})
	for _, path := range []string{"C:", "C:/", "c:/"} {
		full, err := c.localPath([]string{strings.TrimSuffix(path, "/")})
		if err != nil {
			t.Fatalf("browsing into %q: %v", path, err)
		}
		if !strings.EqualFold(filepath.Clean(full), `C:\`) {
			t.Fatalf("browsing into %q resolved to %q, want the C: volume root", path, full)
		}
	}
	// A real escape attempt must still be refused, by segments() before it
	// ever reaches path resolution.
	if _, err := c.ListDir("C:/../.."); err == nil {
		t.Fatalf("expected a %q path to be rejected", "..")
	}
}
