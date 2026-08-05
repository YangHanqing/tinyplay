// Package imagecache keeps poster and backdrop bytes on disk so a media server
// is asked for the same artwork at most once per freshness window instead of
// once per page view. For a media server that is remote rather than on the LAN,
// image fetches dominate how a library feels: this is the difference between a
// poster wall that paints at once and one that trickles in.
//
// Artwork is not immutable — someone can replace a poster on the server — so
// this is a freshness window, not a permanent store. A cached entry has three
// ages:
//
//	< FreshFor   served from disk; the media server is not contacted at all
//	< HardTTL    served from disk immediately and refreshed in the background,
//	             so replaced artwork lands on the next view and nobody waits
//	>= HardTTL   treated as a miss and fetched before anything is served
//
// HardTTL is the ceiling: nothing reaches a viewer from disk beyond that age
// without a successful upstream fetch. A periodic sweep drops entries unused
// for HardTTL and trims least-recently-used bytes past MaxBytes.
//
// Cached bytes belong to the source that produced them, so RemoveServer and
// Clear mirror source deletion and settings reset — artwork from a source the
// user removed must not outlive it on disk.
package imagecache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"tvremote/internal/config"
)

const (
	// FreshFor matches the cadence at which library artwork realistically
	// changes — a library scan or metadata refresh, on the order of half a
	// day. Inside this window no image request leaves the machine.
	FreshFor = 12 * time.Hour

	// HardTTL is the age ceiling for anything served out of this cache.
	HardTTL = 7 * 24 * time.Hour

	// MaxBytes caps the whole directory. Posters run tens of KB and backdrops
	// a few hundred, so this holds a large personal library comfortably while
	// staying a sane footprint in a per-user config directory.
	MaxBytes = 128 << 20

	sweepEvery   = 6 * time.Hour
	dirName      = "image-cache"
	fileSuffix   = ".img"
	headerFormat = 1
)

// maxBackgroundRefreshes bounds how many stale entries are revalidated at once.
// The point of the background refresh is to keep a slow media server off the
// viewer's critical path; opening a connection per poster would put it right
// back on. Acquisition is non-blocking, so a wall of stale posters refreshes a
// few per view and converges over the next few views instead of queueing.
const maxBackgroundRefreshes = 3

// Entry is a cached image ready to serve.
type Entry struct {
	Data        []byte
	ContentType string
	ETag        string // strong validator: hex SHA-256 of Data
}

// Key identifies one rendition of one item's artwork. MaxHeight is part of the
// identity because the media server resizes on request: the tiny swatch the
// remote tab samples for its accent colour and the backdrop behind it are the
// same item but different bytes.
type Key struct {
	ServerID  string
	ItemID    string
	Type      string
	MaxHeight int
}

type header struct {
	Version     int    `json:"v"`
	ContentType string `json:"ct"`
	ETag        string `json:"etag"`
	FetchedAt   int64  `json:"fetched_at"`
}

// Fetch serves k from disk when it can and calls fetch only when it must. The
// bool reports whether there is anything to serve.
//
// A fetch that comes back empty is reported as a miss and is not cached: a
// media server answering 404 for artwork usually means it has not generated
// that image yet, which is a temporary state, and caching it would pin a
// missing poster in place for the whole freshness window.
func Fetch(k Key, fetch func() ([]byte, string)) (Entry, bool) {
	path, ok := entryPath(k)
	if !ok {
		// No writable cache directory. Degrade to a plain proxy rather than
		// failing the request.
		return fresh(fetch)
	}
	sweepIfDue()
	if entry, age, err := read(path); err == nil {
		touch(path)
		if age < FreshFor {
			return entry, true
		}
		if age < HardTTL {
			go refresh(path, fetch)
			return entry, true
		}
	}
	entry, ok := fresh(fetch)
	if !ok {
		return Entry{}, false
	}
	write(path, entry)
	return entry, true
}

// RemoveServer drops everything cached for one source. It mirrors source
// deletion and is best-effort: removing a source must not fail because a stale
// cache file is already gone.
func RemoveServer(serverID string) {
	dir, ok := cacheDir()
	if !ok {
		return
	}
	matches, err := filepath.Glob(filepath.Join(dir, serverTag(serverID)+"-*"+fileSuffix))
	if err != nil {
		return
	}
	for _, path := range matches {
		_ = os.Remove(path)
	}
}

// Clear is the settings-reset companion to RemoveServer.
func Clear() {
	_ = os.RemoveAll(filepath.Join(config.DataDir(), dirName))
}

// ── Keys and paths ───────────────────────────────────────────────────────────

// filename is split into a source tag and a rendition digest so RemoveServer
// can find one source's entries without opening every file.
func (k Key) filename() string {
	sum := sha256.Sum256([]byte(k.ItemID + "\x00" + k.Type + "\x00" + strconv.Itoa(k.MaxHeight)))
	return serverTag(k.ServerID) + "-" + hex.EncodeToString(sum[:]) + fileSuffix
}

func serverTag(serverID string) string {
	sum := sha256.Sum256([]byte(serverID))
	return hex.EncodeToString(sum[:4])
}

func entryPath(k Key) (string, bool) {
	dir, ok := cacheDir()
	if !ok {
		return "", false
	}
	return filepath.Join(dir, k.filename()), true
}

// cacheDir resolves (and creates) the cache directory on every call rather than
// memoising it, because config.DataDir() is itself resolved from the
// environment and tests move it between cases.
func cacheDir() (string, bool) {
	path := filepath.Join(config.DataDir(), dirName)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", false
	}
	return path, true
}

// ── Fetching ─────────────────────────────────────────────────────────────────

func fresh(fetch func() ([]byte, string)) (Entry, bool) {
	data, contentType := fetch()
	if len(data) == 0 {
		return Entry{}, false
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}
	return Entry{Data: data, ContentType: contentType, ETag: etagOf(data)}, true
}

var (
	refreshMu       sync.Mutex
	refreshInflight = map[string]bool{}
	refreshSlots    = make(chan struct{}, maxBackgroundRefreshes)
)

// refresh revalidates a stale entry after its bytes have already been served.
// It is deliberately silent about failure: the viewer has their image, and an
// unreachable server should leave the stale entry in place to be retried on the
// next view rather than evicting a usable poster.
func refresh(path string, fetch func() ([]byte, string)) {
	name := filepath.Base(path)
	refreshMu.Lock()
	if refreshInflight[name] {
		refreshMu.Unlock()
		return
	}
	refreshInflight[name] = true
	refreshMu.Unlock()
	defer func() {
		refreshMu.Lock()
		delete(refreshInflight, name)
		refreshMu.Unlock()
	}()

	select {
	case refreshSlots <- struct{}{}:
	default:
		return
	}
	defer func() { <-refreshSlots }()

	if entry, ok := fresh(fetch); ok {
		write(path, entry)
	}
}

// ── Disk format ──────────────────────────────────────────────────────────────
//
// One file per entry: a single-line JSON header, a newline, then the raw image
// bytes. Keeping the metadata in the file rather than a shared index means a
// read is one open and a write is one atomic rename, with no index to corrupt
// or to serialise every request behind.
//
// The header carries when the bytes were fetched (freshness); the file's mtime
// carries when they were last served (LRU). They move independently, which is
// why the age check never uses mtime.

func read(path string) (Entry, time.Duration, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, 0, err
	}
	newline := bytes.IndexByte(raw, '\n')
	if newline <= 0 {
		return Entry{}, 0, errors.New("imagecache: entry has no header")
	}
	var h header
	if err := json.Unmarshal(raw[:newline], &h); err != nil || h.Version != headerFormat {
		return Entry{}, 0, errors.New("imagecache: unreadable header")
	}
	data := raw[newline+1:]
	if len(data) == 0 {
		return Entry{}, 0, errors.New("imagecache: entry has no body")
	}
	age := time.Since(time.Unix(h.FetchedAt, 0))
	if age < 0 {
		// The clock moved backwards since the write. Treat the entry as new
		// rather than as impossibly old, so a corrected clock doesn't discard
		// a whole cache.
		age = 0
	}
	return Entry{Data: data, ContentType: h.ContentType, ETag: h.ETag}, age, nil
}

func write(path string, entry Entry) {
	head, err := json.Marshal(header{
		Version:     headerFormat,
		ContentType: entry.ContentType,
		ETag:        entry.ETag,
		FetchedAt:   time.Now().Unix(),
	})
	if err != nil {
		return
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".img-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return
	}
	if _, err := tmp.Write(append(head, '\n')); err != nil {
		_ = tmp.Close()
		return
	}
	if _, err := tmp.Write(entry.Data); err != nil {
		_ = tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpName, path)
}

// touch records that an entry was served, which is what the sweep evicts by.
func touch(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

func etagOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ── Sweeping ─────────────────────────────────────────────────────────────────

var (
	sweepMu   sync.Mutex
	lastSweep time.Time
	sweeping  bool
)

// sweepIfDue is called on the request path, so the due-ness check is a mutex
// and nothing else; only an actual sweep, which walks the directory, moves to a
// goroutine. The first image request after launch always sweeps, which is why
// the package needs no timer and no startup hook.
func sweepIfDue() {
	sweepMu.Lock()
	if sweeping || (!lastSweep.IsZero() && time.Since(lastSweep) < sweepEvery) {
		sweepMu.Unlock()
		return
	}
	sweeping = true
	sweepMu.Unlock()

	go func() {
		sweep()
		sweepMu.Lock()
		sweeping = false
		lastSweep = time.Now()
		sweepMu.Unlock()
	}()
}

// sweep enforces the two bounds the cache promises: nothing unused for longer
// than HardTTL, and no more than MaxBytes in total, evicting least recently
// served first.
func sweep() {
	dir, ok := cacheDir()
	if !ok {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type record struct {
		path string
		used time.Time
		size int64
	}
	kept := make([]record, 0, len(entries))
	var total int64
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != fileSuffix {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if time.Since(info.ModTime()) >= HardTTL {
			_ = os.Remove(path)
			continue
		}
		kept = append(kept, record{path: path, used: info.ModTime(), size: info.Size()})
		total += info.Size()
	}
	if total <= MaxBytes {
		return
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].used.Before(kept[j].used) })
	for _, rec := range kept {
		if total <= MaxBytes {
			return
		}
		if err := os.Remove(rec.path); err == nil {
			total -= rec.size
		}
	}
}
