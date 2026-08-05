package plex

import (
	"strings"
	"sync"
	"time"
)

// Plex has no image endpoint addressed by item id. Every poster first needs
// /library/metadata/{id} to learn the item's thumb/art path, and only then can
// /photo/:/transcode fetch the bytes — two round-trips per tile, against a
// server that for many users is remote. Memoising the first half turns a poster
// wall back into one request per image.
//
// The paths Plex hands back carry its own version segment, so a replaced poster
// changes the path rather than the bytes behind it: a stale memo would keep
// serving the old artwork. artKeyTTL is therefore kept at the same half-day
// window the image cache treats artwork as fresh for.
const (
	artKeyTTL = 12 * time.Hour
	// artKeyMax bounds the memo. It holds short strings, so this is generous
	// for any personal library; overflowing simply starts a new generation
	// rather than paying for LRU bookkeeping on a lookup this cheap.
	artKeyMax = 4096
)

type artEntry struct {
	paths  map[string]string
	loaded time.Time
}

var (
	artMu    sync.Mutex
	artCache = map[string]artEntry{}
)

func artCacheKey(serverID, itemID string) string { return serverID + "\x00" + itemID }

func lookupArtPaths(serverID, itemID string) (map[string]string, bool) {
	artMu.Lock()
	defer artMu.Unlock()
	entry, ok := artCache[artCacheKey(serverID, itemID)]
	if !ok || time.Since(entry.loaded) >= artKeyTTL {
		return nil, false
	}
	return entry.paths, true
}

func storeArtPaths(serverID, itemID string, paths map[string]string) {
	artMu.Lock()
	defer artMu.Unlock()
	if len(artCache) >= artKeyMax {
		artCache = map[string]artEntry{}
	}
	artCache[artCacheKey(serverID, itemID)] = artEntry{paths: paths, loaded: time.Now()}
}

// forgetArtPaths drops a memo whose paths no longer fetch, so the next request
// pays for one metadata lookup instead of failing the same way forever.
func forgetArtPaths(serverID, itemID string) {
	artMu.Lock()
	defer artMu.Unlock()
	delete(artCache, artCacheKey(serverID, itemID))
}

// ForgetServer drops one source's memoised artwork paths. They are only paths,
// but they name the items in a library, so a removed source should not leave
// them behind.
func ForgetServer(serverID string) {
	artMu.Lock()
	defer artMu.Unlock()
	prefix := serverID + "\x00"
	for key := range artCache {
		if strings.HasPrefix(key, prefix) {
			delete(artCache, key)
		}
	}
}

// ForgetAll is the settings-reset companion to ForgetServer.
func ForgetAll() {
	artMu.Lock()
	defer artMu.Unlock()
	artCache = map[string]artEntry{}
}
