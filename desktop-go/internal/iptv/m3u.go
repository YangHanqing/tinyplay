package iptv

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var (
	attrRe    = regexp.MustCompile(`([A-Za-z0-9_-]+)="([^"]*)"`)
	qualityRe = regexp.MustCompile(`(?i)\b(4K|UHD|FHD|1080P?|HD|SD)\b`)
)

type pendingEntry struct {
	attrs map[string]string
	name  string
}

// ParseM3U parses an M3U/M3U8 playlist. It only understands the common
// #EXTM3U/#EXTINF attributes (tvg-id, tvg-name, tvg-logo, group-title) — VLC-
// or Kodi-specific tags (#EXTVLCOPT, #KODIPROP, #EXTGRP, catchup hints, ...)
// are intentionally ignored, not an oversight (see plan non-goals).
//
// epgHint is the #EXTM3U header's x-tvg-url/url-tvg attribute, used by the
// caller only as a fallback when the source has no epg_url configured.
func ParseM3U(r io.Reader) (channels []Channel, epgHint string, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)

	order := []string{}
	byID := map[string]*Channel{}
	variantCount := map[string]int{}

	var pending *pendingEntry
	sawHeader := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if !sawHeader {
			sawHeader = true
			if strings.HasPrefix(line, "#EXTM3U") {
				attrs := parseAttrs(line)
				epgHint = firstNonEmpty(attrs["x-tvg-url"], attrs["url-tvg"])
				continue
			}
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			rest := line[len("#EXTINF:"):]
			attrPart, name := rest, ""
			if idx := strings.LastIndex(rest, ","); idx >= 0 {
				attrPart, name = rest[:idx], strings.TrimSpace(rest[idx+1:])
			}
			pending = &pendingEntry{attrs: parseAttrs(attrPart), name: name}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if pending == nil {
			continue // a stream URL with no preceding #EXTINF is not usable
		}
		addEntry(pending, line, &order, byID, variantCount)
		pending = nil
	}
	if err = sc.Err(); err != nil {
		return nil, "", err
	}
	channels = make([]Channel, 0, len(order))
	for _, id := range order {
		channels = append(channels, *byID[id])
	}
	return channels, epgHint, nil
}

func parseAttrs(s string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(s, -1) {
		out[strings.ToLower(m[1])] = m[2]
	}
	return out
}

func addEntry(e *pendingEntry, url string, order *[]string, byID map[string]*Channel, variantCount map[string]int) {
	tvgID := e.attrs["tvg-id"]
	name := firstNonEmpty(e.name, e.attrs["tvg-name"])
	group := e.attrs["group-title"]
	id := channelID(tvgID, name, group)
	quality := parseQuality(name)

	variantCount[id]++
	label := quality
	if label == "" {
		label = fmt.Sprintf("#%d", variantCount[id])
	}
	variant := StreamVariant{URL: url, Label: label}

	if existing, ok := byID[id]; ok {
		existing.Variants = append(existing.Variants, variant)
		return
	}
	byID[id] = &Channel{
		ID:         id,
		Name:       name,
		LogoURL:    e.attrs["tvg-logo"],
		GroupTitle: group,
		TvgID:      tvgID,
		Quality:    quality,
		Variants:   []StreamVariant{variant},
	}
	*order = append(*order, id)
}

func parseQuality(name string) string {
	m := strings.ToUpper(qualityRe.FindString(name))
	if strings.HasPrefix(m, "1080") {
		return "FHD"
	}
	return m
}
