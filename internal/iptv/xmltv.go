package iptv

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const maxEPGProgrammes = 500_000

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
	dec.CharsetReader = xmlCharsetReader
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
		if !stop.After(start) {
			continue
		}
		if len(out) >= maxEPGProgrammes {
			return nil, errf(502, "The EPG has too many programmes")
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

// XMLTV feeds are often declared as UTF-8 but a number of long-lived Chinese
// providers still declare/serve GBK or GB18030. Keep the supported set narrow
// and explicit; unknown charsets should fail rather than being silently
// mis-decoded into a plausible but wrong programme guide.
func xmlCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return input, nil
	case "gbk", "gb2312", "gb18030", "cp936":
		return transform.NewReader(input, simplifiedchinese.GB18030.NewDecoder()), nil
	case "windows-1252", "cp1252", "iso-8859-1", "latin1":
		return transform.NewReader(input, charmap.Windows1252.NewDecoder()), nil
	default:
		return nil, fmt.Errorf("unsupported XMLTV charset %q", charset)
	}
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
