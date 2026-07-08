package iptv

import (
	"bufio"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"strings"
)

// maybeGunzip transparently decompresses gzip-encoded input (common for
// large hosted XMLTV feeds, e.g. guide.xml.gz), detected by magic bytes
// rather than by URL suffix since redirects/CDNs can hide the extension.
func maybeGunzip(r io.Reader) (io.Reader, error) {
	br := bufio.NewReader(r)
	magic, err := br.Peek(2)
	if err != nil && err != io.EOF {
		return br, nil
	}
	if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		return gzip.NewReader(br)
	}
	return br, nil
}

// channelID derives a stable id for a playlist entry: tvg-id when present
// (the common case, and required for EPG matching), else a short hash of
// name+group so favorites/recents still survive a playlist refresh as long
// as the channel's name and category don't change upstream.
func channelID(tvgID, name, group string) string {
	if tvgID != "" {
		return "tvg:" + strings.ToLower(tvgID)
	}
	h := sha1.Sum([]byte(strings.ToLower(name) + "|" + strings.ToLower(group)))
	return "h:" + hex.EncodeToString(h[:])[:12]
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
