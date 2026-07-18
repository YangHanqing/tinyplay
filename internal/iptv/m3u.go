package iptv

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	// Covers normal quoted attributes plus the single-quoted/unquoted forms
	// emitted by many Xtream-style list generators.
	attrRe    = regexp.MustCompile(`([A-Za-z0-9_-]+)=(?:"([^"]*)"|'([^']*)'|([^,\s]+))`)
	qualityRe = regexp.MustCompile(`(?i)\b(4K|UHD|FHD|1080P?|HD|SD)\b`)
)

const (
	maxPlaylistChannels = 100_000
	maxPlaylistVariants = 200_000
)

type pendingEntry struct {
	attrs   map[string]string
	name    string
	headers map[string]string
	group   string
}

// ParseM3U keeps the original small public API for callers/tests that only
// need the x-tvg-url string. Source loading uses ParseM3UWithResources below
// to preserve request headers and resolve relative URLs safely.
func ParseM3U(r io.Reader) (channels []Channel, epgHint string, err error) {
	channels, hint, err := ParseM3UWithResources(r, nil, nil)
	if hint != nil && hint.URL != nil {
		epgHint = hint.URL.String()
	}
	return channels, epgHint, err
}

// ParseM3UWithResources tolerates the useful M3U extensions found in real
// providers (relative URLs, EXTGRP, VLC/EXTHTTP request headers and archive
// hints) while keeping input bounded and credential forwarding constrained.
func ParseM3UWithResources(r io.Reader, playlistURL *url.URL,
	inheritedHeaders map[string]string) (channels []Channel, epgHint *ResourceRequest, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	order := []string{}
	byID := map[string]*Channel{}
	variantCount := map[string]int{}
	var totalVariants int

	var pending *pendingEntry
	sawHeader := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if !sawHeader {
			sawHeader = true
			if strings.HasPrefix(upper, "#EXTM3U") {
				attrs := parseAttrs(line)
				if raw := firstNonEmpty(attrs["x-tvg-url"], attrs["url-tvg"], attrs["tvg-url"]); raw != "" {
					if resource, resourceErr := parseResourceRequest(raw, playlistURL); resourceErr == nil {
						epgHint = &resource
					}
				}
				continue
			}
		}
		if strings.HasPrefix(upper, "#EXTINF:") {
			rest := line[len("#EXTINF:"):]
			attrPart, name := rest, ""
			if idx := strings.LastIndex(rest, ","); idx >= 0 {
				attrPart, name = rest[:idx], strings.TrimSpace(rest[idx+1:])
			}
			attrs := parseAttrs(attrPart)
			pending = &pendingEntry{attrs: attrs, name: name, headers: headersFromAttrs(attrs)}
			continue
		}
		if pending != nil && strings.HasPrefix(upper, "#EXTGRP:") {
			pending.group = strings.TrimSpace(line[len("#EXTGRP:"):])
			continue
		}
		if pending != nil && strings.HasPrefix(upper, "#EXTVLCOPT:") {
			applyHeaderOption(line[len("#EXTVLCOPT:"):], pending.headers)
			continue
		}
		if pending != nil && strings.HasPrefix(upper, "#EXTHTTP:") {
			mergeHeaders(pending.headers, headersFromJSON(line[len("#EXTHTTP:"):]))
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if pending == nil {
			continue // stream URLs without metadata are not useful channel rows
		}

		resource, resourceErr := parseResourceRequest(line, playlistURL)
		if resourceErr == nil {
			if err := addEntry(pending, resource, playlistURL, inheritedHeaders, &order, byID, variantCount, &totalVariants); err != nil {
				return nil, nil, err
			}
		}
		pending = nil
	}
	if err = sc.Err(); err != nil {
		return nil, nil, err
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
		value := firstNonEmpty(m[2], m[3], m[4])
		out[strings.ToLower(m[1])] = value
	}
	return out
}

func headersFromAttrs(attrs map[string]string) map[string]string {
	out := map[string]string{}
	for name, value := range attrs {
		addAllowedHeader(out, name, value)
	}
	return out
}

func headersFromJSON(raw string) map[string]string {
	var values map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &values) != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for name, value := range values {
		switch v := value.(type) {
		case string:
			addAllowedHeader(out, name, v)
		case float64:
			addAllowedHeader(out, name, fmt.Sprint(v))
		}
	}
	return out
}

func applyHeaderOption(raw string, headers map[string]string) {
	name, value, ok := strings.Cut(strings.TrimSpace(raw), "=")
	if ok {
		addAllowedHeader(headers, name, value)
	}
}

func addEntry(e *pendingEntry, resource ResourceRequest, playlistURL *url.URL,
	inheritedHeaders map[string]string, order *[]string, byID map[string]*Channel,
	variantCount map[string]int, totalVariants *int) error {
	if *totalVariants >= maxPlaylistVariants {
		return errf(502, "The playlist has too many stream variants")
	}
	tvgID := e.attrs["tvg-id"]
	name := firstNonEmpty(e.name, e.attrs["tvg-name"])
	group := firstNonEmpty(e.attrs["group-title"], e.group)
	if strings.TrimSpace(name) == "" || resource.URL == nil {
		return nil
	}
	id := channelID(tvgID, name, group)
	quality := parseQuality(name)

	headers := inheritedHeadersForStream(inheritedHeaders, playlistURL, resource.URL)
	mergeHeaders(headers, e.headers)
	mergeHeaders(headers, resource.HTTPHeaders)

	variantCount[id]++
	*totalVariants++
	label := quality
	if label == "" {
		label = fmt.Sprintf("#%d", variantCount[id])
	}
	variant := StreamVariant{URL: resource.URL.String(), Label: label, HTTPHeaders: headers}

	if existing, ok := byID[id]; ok {
		existing.Variants = append(existing.Variants, variant)
		return nil
	}
	if len(byID) >= maxPlaylistChannels {
		return errf(502, "The playlist has too many channels")
	}
	shift, _ := strconv.ParseFloat(strings.TrimSpace(e.attrs["tvg-shift"]), 64)
	catchupDays, _ := strconv.Atoi(strings.TrimSpace(firstNonEmpty(e.attrs["catchup-days"], e.attrs["timeshift"])))
	if catchupDays < 0 {
		catchupDays = 0
	}
	catchupSource := strings.TrimSpace(e.attrs["catchup-source"])
	catchupHeaders := map[string]string{}
	if catchupSource != "" {
		if catchupResource, err := parseResourceRequest(catchupSource, playlistURL); err == nil && catchupResource.URL != nil {
			catchupSource = catchupResource.URL.String()
			catchupHeaders = catchupResource.HTTPHeaders
		} else {
			// A malformed archive hint must not make an otherwise-playable live
			// entry disappear. Leave replay unavailable for this channel instead.
			catchupSource = ""
		}
	}
	byID[id] = &Channel{
		ID:             id,
		Name:           name,
		LogoURL:        e.attrs["tvg-logo"],
		GroupTitle:     group,
		TvgID:          tvgID,
		Quality:        quality,
		EPGShiftHours:  shift,
		Catchup:        strings.ToLower(strings.TrimSpace(e.attrs["catchup"])),
		CatchupSource:  catchupSource,
		CatchupHeaders: catchupHeaders,
		CatchupDays:    catchupDays,
		Variants:       []StreamVariant{variant},
	}
	*order = append(*order, id)
	return nil
}

func parseQuality(name string) string {
	m := strings.ToUpper(qualityRe.FindString(name))
	if strings.HasPrefix(m, "1080") {
		return "FHD"
	}
	return m
}
