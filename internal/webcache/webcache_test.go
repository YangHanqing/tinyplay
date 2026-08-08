package webcache

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates parent directories and a file of exactly size bytes.
func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// newProfile lays out a realistic WebView2 profile: the runtime's EBWebView
// wrapper, a Default profile inside it, and both kinds of data mixed together.
func newProfile(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "EBWebView", "Default")

	// Derived — should be reclaimed.
	writeFile(t, filepath.Join(p, "Cache", "Cache_Data", "f_000001"), 1000)
	writeFile(t, filepath.Join(p, "Code Cache", "js", "index"), 200)
	writeFile(t, filepath.Join(p, "GPUCache", "data_0"), 300)
	writeFile(t, filepath.Join(p, "Service Worker", "CacheStorage", "abc", "blob"), 4000)
	writeFile(t, filepath.Join(p, "Service Worker", "ScriptCache", "index"), 500)

	// User data — must survive.
	writeFile(t, filepath.Join(p, "Network", "Cookies"), 50)
	writeFile(t, filepath.Join(p, "Local Storage", "leveldb", "000003.log"), 60)
	writeFile(t, filepath.Join(p, "IndexedDB", "site_0.indexeddb", "CURRENT"), 70)
	writeFile(t, filepath.Join(p, "Session Storage", "000004.log"), 80)
	writeFile(t, filepath.Join(p, "Service Worker", "Database", "000005.log"), 90)
	writeFile(t, filepath.Join(p, "Preferences"), 10)
	writeFile(t, filepath.Join(p, "Login Data"), 20)
	return root
}

const derivedTotal = 1000 + 200 + 300 + 4000 + 500

// The whole point of the package: clearing frees the cache and leaves the
// login state on disk. If this test ever needs relaxing, the feature's promise
// to the user ("site logins are kept") has to change with it.
func TestClearKeepsCookiesAndSiteData(t *testing.T) {
	root := newProfile(t)
	p := filepath.Join(root, "EBWebView", "Default")

	freed, err := Clear(root)
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if freed != derivedTotal {
		t.Errorf("freed = %d, want %d", freed, derivedTotal)
	}

	mustExist := []string{
		filepath.Join(p, "Network", "Cookies"),
		filepath.Join(p, "Local Storage", "leveldb", "000003.log"),
		filepath.Join(p, "IndexedDB", "site_0.indexeddb", "CURRENT"),
		filepath.Join(p, "Session Storage", "000004.log"),
		filepath.Join(p, "Service Worker", "Database", "000005.log"),
		filepath.Join(p, "Preferences"),
		filepath.Join(p, "Login Data"),
	}
	for _, path := range mustExist {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("user data was deleted: %s", path)
		}
	}

	mustBeGone := []string{
		filepath.Join(p, "Cache"),
		filepath.Join(p, "Code Cache"),
		filepath.Join(p, "GPUCache"),
		filepath.Join(p, "Service Worker", "CacheStorage"),
		filepath.Join(p, "Service Worker", "ScriptCache"),
	}
	for _, path := range mustBeGone {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("cache directory survived: %s", path)
		}
	}
}

// Usage must report what Clear would reclaim, not the size of the profile.
// A menu that says "Clear cache (1.2 GB)" and then frees 400MB is lying.
func TestUsageCountsOnlyReclaimableBytes(t *testing.T) {
	root := newProfile(t)
	used, err := Usage(root)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if used != derivedTotal {
		t.Errorf("Usage = %d, want %d (user data must not be counted)", used, derivedTotal)
	}

	freed, err := Clear(root)
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if freed != used {
		t.Errorf("Clear freed %d but Usage promised %d", freed, used)
	}
}

// The profile is created lazily by the first browser window, so a root that
// does not exist yet is an ordinary state and must not surface as an error.
func TestMissingProfileIsZeroNotError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "never-opened")
	used, err := Usage(root)
	if err != nil {
		t.Fatalf("Usage on missing root: %v", err)
	}
	if used != 0 {
		t.Errorf("Usage = %d, want 0", used)
	}
	if _, err := Clear(root); err != nil {
		t.Fatalf("Clear on missing root: %v", err)
	}
}

func TestEnforceLeavesCacheWithinBudget(t *testing.T) {
	root := newProfile(t)
	freed, err := Enforce(root, derivedTotal+1)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if freed != 0 {
		t.Errorf("freed %d bytes while under budget", freed)
	}
	if used, _ := Usage(root); used != derivedTotal {
		t.Errorf("Usage = %d after a no-op Enforce, want %d", used, derivedTotal)
	}
}

func TestEnforceClearsWhenOverBudget(t *testing.T) {
	root := newProfile(t)
	freed, err := Enforce(root, derivedTotal-1)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if freed != derivedTotal {
		t.Errorf("freed = %d, want %d", freed, derivedTotal)
	}
	// Even the over-budget path must not touch logins.
	cookies := filepath.Join(root, "EBWebView", "Default", "Network", "Cookies")
	if _, err := os.Stat(cookies); err != nil {
		t.Errorf("Enforce deleted cookies: %v", err)
	}
}

// "Unlimited" reaches Enforce as a non-positive limit. It must mean "never
// sweep", not "budget of zero, delete everything" — the inverted reading would
// wipe the cache on every launch for users who explicitly turned the cap off.
func TestEnforceUnlimitedNeverClears(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		root := newProfile(t)
		freed, err := Enforce(root, limit)
		if err != nil {
			t.Fatalf("Enforce(%d): %v", limit, err)
		}
		if freed != 0 {
			t.Errorf("Enforce(%d) freed %d bytes, want 0", limit, freed)
		}
		if used, _ := Usage(root); used != derivedTotal {
			t.Errorf("Enforce(%d) reduced usage to %d", limit, used)
		}
	}
}

// A directory whose name is not on the allow-list is left alone even when it
// looks cache-shaped. Matching by name is what keeps the blast radius known.
func TestUnknownDirectoriesAreLeftAlone(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "EBWebView", "Default")
	writeFile(t, filepath.Join(p, "SomeFutureCache", "data"), 500)
	writeFile(t, filepath.Join(p, "Cache", "f_000001"), 100)

	freed, err := Clear(root)
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if freed != 100 {
		t.Errorf("freed = %d, want 100", freed)
	}
	if _, err := os.Stat(filepath.Join(p, "SomeFutureCache", "data")); err != nil {
		t.Errorf("an unrecognised directory was deleted: %v", err)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{20 << 20, "20 MB"},
		{1288490188, "1.2 GB"},
		{2 << 30, "2.0 GB"},
	}
	for _, tc := range cases {
		if got := FormatBytes(tc.in); got != tc.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
