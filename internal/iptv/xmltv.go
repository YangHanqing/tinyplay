package iptv

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

type xmltvProgramme struct {
	Channel  string `xml:"channel,attr"`
	Start    string `xml:"start,attr"`
	Stop     string `xml:"stop,attr"`
	Title    string `xml:"title"`
	SubTitle string `xml:"sub-title"`
	Desc     string `xml:"desc"`
}

// ParseXMLTV streams an XMLTV document and extracts <programme> entries,
// skipping malformed ones rather than failing the whole feed (public EPG
// sources routinely have a handful of bad entries mixed into otherwise-good
// data). It does not require <channel> elements at all — matching against
// playlist channels happens by tvg-id/channel attribute equality only.
func ParseXMLTV(r io.Reader) ([]Programme, error) {
	dec := xml.NewDecoder(r)
	var out []Programme
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "programme" {
			continue
		}
		var p xmltvProgramme
		if err := dec.DecodeElement(&p, &se); err != nil {
			continue
		}
		start, errStart := parseXMLTVTime(p.Start)
		stop, errStop := parseXMLTVTime(p.Stop)
		if errStart != nil || errStop != nil || p.Channel == "" {
			continue
		}
		out = append(out, Programme{
			ChannelID: strings.ToLower(strings.TrimSpace(p.Channel)),
			Start:     start,
			Stop:      stop,
			Title:     strings.TrimSpace(p.Title),
			SubTitle:  strings.TrimSpace(p.SubTitle),
			Desc:      strings.TrimSpace(p.Desc),
		})
	}
	return out, nil
}

// parseXMLTVTime parses the XMLTV timestamp format, e.g. "20260706083000
// +0800", tolerating the space-less and no-timezone variants some feeds emit.
func parseXMLTVTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"20060102150405 -0700", "20060102150405-0700", "20060102150405"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid xmltv time %q", s)
}
