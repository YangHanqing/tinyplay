package imagecache

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The cache's whole promise is about age, so every test needs to place an entry
// at a chosen age. Rewriting the header is how: it is the only record of when
// the bytes were fetched, and it is deliberately independent of the file's
// mtime, which records when they were last served.
func ageEntry(t *testing.T, k Key, age time.Duration) {
	t.Helper()
	path, ok := entryPath(k)
	if !ok {
		t.Fatal("no cache directory")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	newline := bytes.IndexByte(raw, '\n')
	if newline <= 0 {
		t.Fatal("entry has no header")
	}
	var h header
	if err := json.Unmarshal(raw[:newline], &h); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	h.FetchedAt = time.Now().Add(-age).Unix()
	head, err := json.Marshal(h)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	rewritten := append(append(head, '\n'), raw[newline+1:]...)
	if err := os.WriteFile(path, rewritten, 0o600); err != nil {
		t.Fatalf("write entry: %v", err)
	}
}

func useTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TVREMOTE_DATA_DIR", dir)
	// A previous test may have left the package-level sweep clock set; the
	// sweep is otherwise timer-gated and would skip a fresh directory.
	sweepMu.Lock()
	lastSweep = time.Time{}
	sweepMu.Unlock()
	return dir
}

// counter is a fetch function that records how often the media server was
// actually asked — the number every test here is really asserting on.
type counter struct {
	mu    sync.Mutex
	calls int
	data  []byte
	ct    string
	done  chan struct{}
}

func (c *counter) fetch() ([]byte, string) {
	c.mu.Lock()
	c.calls++
	data, ct := c.data, c.ct
	done := c.done
	c.mu.Unlock()
	if done != nil {
		select {
		case done <- struct{}{}:
		default:
		}
	}
	return data, ct
}

func (c *counter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func testKey() Key {
	return Key{ServerID: "srv-1", ItemID: "item-1", Type: "Primary", MaxHeight: 400}
}

func TestFreshEntryNeverAsksTheServer(t *testing.T) {
	useTempDir(t)
	c := &counter{data: []byte("poster-bytes"), ct: "image/jpeg"}
	key := testKey()

	entry, ok := Fetch(key, c.fetch)
	if !ok || string(entry.Data) != "poster-bytes" {
		t.Fatalf("first fetch = %q, %v", entry.Data, ok)
	}
	if entry.ContentType != "image/jpeg" {
		t.Errorf("content type = %q", entry.ContentType)
	}

	for i := 0; i < 5; i++ {
		entry, ok = Fetch(key, c.fetch)
		if !ok || string(entry.Data) != "poster-bytes" {
			t.Fatalf("cached fetch %d = %q, %v", i, entry.Data, ok)
		}
	}
	if got := c.count(); got != 1 {
		t.Errorf("upstream fetches = %d, want 1", got)
	}
}

// The point of the stale window: a viewer gets bytes at once and the slow media
// server is contacted off the critical path.
func TestStaleEntryServesImmediatelyThenRefreshes(t *testing.T) {
	useTempDir(t)
	key := testKey()
	c := &counter{data: []byte("old"), ct: "image/jpeg"}
	if _, ok := Fetch(key, c.fetch); !ok {
		t.Fatal("seed fetch failed")
	}
	ageEntry(t, key, FreshFor+time.Hour)

	c.mu.Lock()
	c.data = []byte("new")
	c.done = make(chan struct{}, 1)
	c.mu.Unlock()

	entry, ok := Fetch(key, c.fetch)
	if !ok || string(entry.Data) != "old" {
		t.Fatalf("stale read = %q, %v; want the cached bytes served immediately", entry.Data, ok)
	}

	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		t.Fatal("background refresh never ran")
	}
	// The refresh writes after fetch returns; wait for the bytes to land.
	deadline := time.Now().Add(2 * time.Second)
	for {
		entry, ok = Fetch(key, c.fetch)
		if ok && string(entry.Data) == "new" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refreshed entry = %q, want %q", entry.Data, "new")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// HardTTL is the ceiling the cache promises: past it, nothing is served without
// a successful fetch first.
func TestBeyondHardTTLFetchesBeforeServing(t *testing.T) {
	useTempDir(t)
	key := testKey()
	c := &counter{data: []byte("old"), ct: "image/jpeg"}
	if _, ok := Fetch(key, c.fetch); !ok {
		t.Fatal("seed fetch failed")
	}
	ageEntry(t, key, HardTTL+time.Hour)

	c.mu.Lock()
	c.data = []byte("new")
	c.mu.Unlock()

	entry, ok := Fetch(key, c.fetch)
	if !ok || string(entry.Data) != "new" {
		t.Fatalf("expired read = %q, %v; want a synchronous refetch", entry.Data, ok)
	}
	if got := c.count(); got != 2 {
		t.Errorf("upstream fetches = %d, want 2", got)
	}
}

// A media server that has not generated artwork yet answers empty. Caching that
// would pin a missing poster in place for the whole freshness window.
func TestMissingImageIsNotCached(t *testing.T) {
	useTempDir(t)
	key := testKey()
	c := &counter{data: nil, ct: ""}

	if _, ok := Fetch(key, c.fetch); ok {
		t.Fatal("empty fetch reported as servable")
	}
	if _, ok := Fetch(key, c.fetch); ok {
		t.Fatal("empty fetch reported as servable on retry")
	}
	if got := c.count(); got != 2 {
		t.Errorf("upstream fetches = %d, want 2 (nothing cached)", got)
	}

	c.mu.Lock()
	c.data = []byte("poster")
	c.ct = "image/png"
	c.mu.Unlock()
	entry, ok := Fetch(key, c.fetch)
	if !ok || string(entry.Data) != "poster" || entry.ContentType != "image/png" {
		t.Fatalf("recovered fetch = %q/%q, %v", entry.Data, entry.ContentType, ok)
	}
}

// Item ids are only unique within a source, and the same item is cached once
// per rendition, so both have to be part of the identity.
func TestKeyIdentitySeparatesSourcesAndRenditions(t *testing.T) {
	useTempDir(t)
	a := Key{ServerID: "srv-a", ItemID: "1", Type: "Primary", MaxHeight: 400}
	b := Key{ServerID: "srv-b", ItemID: "1", Type: "Primary", MaxHeight: 400}
	tall := Key{ServerID: "srv-a", ItemID: "1", Type: "Primary", MaxHeight: 700}
	backdrop := Key{ServerID: "srv-a", ItemID: "1", Type: "Backdrop", MaxHeight: 400}

	seen := map[string]Key{}
	for _, k := range []Key{a, b, tall, backdrop} {
		name := k.filename()
		if prev, dup := seen[name]; dup {
			t.Fatalf("%+v and %+v share a cache file", prev, k)
		}
		seen[name] = k
	}

	for _, k := range []Key{a, b, tall, backdrop} {
		body := k.ServerID + "/" + k.Type
		c := &counter{data: []byte(body), ct: "image/jpeg"}
		if entry, ok := Fetch(k, c.fetch); !ok || string(entry.Data) != body {
			t.Fatalf("%+v served %q", k, entry.Data)
		}
	}
	for _, k := range []Key{a, b, tall, backdrop} {
		body := k.ServerID + "/" + k.Type
		c := &counter{data: []byte("wrong"), ct: "image/jpeg"}
		entry, ok := Fetch(k, c.fetch)
		if !ok || string(entry.Data) != body {
			t.Fatalf("%+v re-read %q, want %q", k, entry.Data, body)
		}
		if c.count() != 0 {
			t.Errorf("%+v was refetched", k)
		}
	}
}

// Artwork belongs to the source that produced it: deleting a source must take
// its cached bytes with it, and must not touch anything else.
func TestRemoveServerDropsOnlyThatSource(t *testing.T) {
	dir := useTempDir(t)
	keep := Key{ServerID: "keep", ItemID: "1", Type: "Primary", MaxHeight: 400}
	drop := Key{ServerID: "drop", ItemID: "1", Type: "Primary", MaxHeight: 400}
	for _, k := range []Key{keep, drop} {
		c := &counter{data: []byte(k.ServerID), ct: "image/jpeg"}
		if _, ok := Fetch(k, c.fetch); !ok {
			t.Fatalf("seed %+v failed", k)
		}
	}

	RemoveServer("drop")

	files, err := filepath.Glob(filepath.Join(dir, dirName, "*"+fileSuffix))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files after removal = %d, want 1", len(files))
	}
	if !strings.HasSuffix(files[0], keep.filename()) {
		t.Errorf("surviving file = %s, want %s", filepath.Base(files[0]), keep.filename())
	}

	c := &counter{data: []byte("refetched"), ct: "image/jpeg"}
	if entry, _ := Fetch(drop, c.fetch); string(entry.Data) != "refetched" || c.count() != 1 {
		t.Errorf("removed source was still served from cache")
	}
}

func TestClearRemovesEverything(t *testing.T) {
	dir := useTempDir(t)
	for _, id := range []string{"a", "b"} {
		k := Key{ServerID: id, ItemID: "1", Type: "Primary", MaxHeight: 400}
		c := &counter{data: []byte(id), ct: "image/jpeg"}
		if _, ok := Fetch(k, c.fetch); !ok {
			t.Fatalf("seed %s failed", id)
		}
	}

	Clear()

	if _, err := os.Stat(filepath.Join(dir, dirName)); !os.IsNotExist(err) {
		t.Errorf("cache directory survived Clear: %v", err)
	}
}

// The sweep enforces the two bounds the package documents. mtime is "last
// served", so an entry nobody has looked at for HardTTL goes even if its bytes
// were refreshed recently.
func TestSweepDropsUnusedEntries(t *testing.T) {
	dir := useTempDir(t)
	stale := Key{ServerID: "s", ItemID: "stale", Type: "Primary", MaxHeight: 400}
	recent := Key{ServerID: "s", ItemID: "recent", Type: "Primary", MaxHeight: 400}
	for _, k := range []Key{stale, recent} {
		c := &counter{data: []byte(k.ItemID), ct: "image/jpeg"}
		if _, ok := Fetch(k, c.fetch); !ok {
			t.Fatalf("seed %+v failed", k)
		}
	}
	stalePath := filepath.Join(dir, dirName, stale.filename())
	old := time.Now().Add(-HardTTL - time.Hour)
	if err := os.Chtimes(stalePath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	sweep()

	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("unused entry survived the sweep: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, dirName, recent.filename())); err != nil {
		t.Errorf("recently served entry was swept: %v", err)
	}
}

func TestETagIsStableAndContentAddressed(t *testing.T) {
	useTempDir(t)
	key := testKey()
	c := &counter{data: []byte("poster"), ct: "image/jpeg"}
	first, _ := Fetch(key, c.fetch)
	second, _ := Fetch(key, c.fetch)
	if first.ETag == "" || first.ETag != second.ETag {
		t.Fatalf("etag not stable across reads: %q vs %q", first.ETag, second.ETag)
	}

	other := Key{ServerID: "srv-1", ItemID: "item-2", Type: "Primary", MaxHeight: 400}
	d := &counter{data: []byte("different poster"), ct: "image/jpeg"}
	differing, _ := Fetch(other, d.fetch)
	if differing.ETag == first.ETag {
		t.Error("different bytes produced the same etag")
	}
}

// A corrupt or truncated file must read as a miss, not as a served empty image.
func TestUnreadableEntryFallsBackToFetching(t *testing.T) {
	dir := useTempDir(t)
	key := testKey()
	c := &counter{data: []byte("poster"), ct: "image/jpeg"}
	if _, ok := Fetch(key, c.fetch); !ok {
		t.Fatal("seed fetch failed")
	}
	path := filepath.Join(dir, dirName, key.filename())
	if err := os.WriteFile(path, []byte("not an entry"), 0o600); err != nil {
		t.Fatalf("corrupt entry: %v", err)
	}

	entry, ok := Fetch(key, c.fetch)
	if !ok || string(entry.Data) != "poster" {
		t.Fatalf("corrupt entry = %q, %v", entry.Data, ok)
	}
	if c.count() != 2 {
		t.Errorf("upstream fetches = %d, want 2", c.count())
	}
}

// A clock that moved backwards should not discard a whole cache.
func TestFutureTimestampIsTreatedAsFresh(t *testing.T) {
	useTempDir(t)
	key := testKey()
	c := &counter{data: []byte("poster"), ct: "image/jpeg"}
	if _, ok := Fetch(key, c.fetch); !ok {
		t.Fatal("seed fetch failed")
	}
	ageEntry(t, key, -48*time.Hour)

	if entry, ok := Fetch(key, c.fetch); !ok || string(entry.Data) != "poster" {
		t.Fatalf("future-dated entry = %q, %v", entry.Data, ok)
	}
	if c.count() != 1 {
		t.Errorf("upstream fetches = %d, want 1", c.count())
	}
}
