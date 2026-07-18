// Package iptv parses M3U/M3U8 playlists and optional XMLTV EPG feeds into a
// channel list, and caches the parsed result per IPTV server so REST handlers
// never re-parse on every request.
package iptv

import "time"

// StreamVariant is one playable URL for a channel (a playlist can list the
// same channel at multiple qualities/mirrors as separate #EXTINF entries).
type StreamVariant struct {
	URL         string            `json:"url"`
	Label       string            `json:"label,omitempty"`
	HTTPHeaders map[string]string `json:"headers,omitempty"`
}

// Channel is one playlist entry, deduplicated across variants sharing the
// same derived id (see channelID in m3u.go).
type Channel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	LogoURL    string `json:"logo_url,omitempty"`
	GroupTitle string `json:"group_title,omitempty"`
	TvgID      string `json:"tvg_id,omitempty"`
	Quality    string `json:"quality,omitempty"`
	// EPGShiftHours is the provider-declared guide offset (tvg-shift). It is
	// applied only while rendering the guide; the cached XMLTV feed remains in
	// its original timezone so multiple channels can share it safely.
	EPGShiftHours float64 `json:"epg_shift_hours,omitempty"`
	// Catchup fields retain the common M3U archive metadata. They are harmless
	// for ordinary live entries and let callers expose replay where a provider
	// actually advertises a valid archive URL.
	Catchup        string            `json:"catchup,omitempty"`
	CatchupSource  string            `json:"catchup_source,omitempty"`
	CatchupHeaders map[string]string `json:"catchup_headers,omitempty"`
	CatchupDays    int               `json:"catchup_days,omitempty"`
	Variants       []StreamVariant   `json:"variants"`
}

// Programme is one XMLTV <programme> entry.
type Programme struct {
	ChannelID string    `json:"channel_id"`
	Start     time.Time `json:"start"`
	Stop      time.Time `json:"stop"`
	Title     string    `json:"title"`
	SubTitle  string    `json:"sub_title,omitempty"`
	Desc      string    `json:"desc,omitempty"`
}
