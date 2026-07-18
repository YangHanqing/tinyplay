package iptv

import (
	"strconv"
	"strings"
	"time"
)

const (
	maximumCatchupDuration = 12 * time.Hour
	catchupFutureAllowance = 5 * time.Minute
)

// CatchupStream turns the archive metadata carried by a channel's M3U entry
// into one private, playable stream. It intentionally supports only the
// widely-used UTC start/end/duration placeholders; a provider without a
// catchup-source remains a normal live channel instead of guessing a URL.
func (c *Client) CatchupStream(channelID string, variantIndex int, start, stop time.Time) (StreamVariant, error) {
	channel := c.ChannelByID(channelID)
	if channel == nil || channel.CatchupSource == "" || channel.CatchupDays <= 0 {
		return StreamVariant{}, errf(400, "Catch-up is not available for this channel")
	}
	if start.IsZero() || stop.IsZero() || !stop.After(start) || stop.Sub(start) > maximumCatchupDuration {
		return StreamVariant{}, errf(400, "Invalid programme time range")
	}
	now := time.Now()
	if start.After(now.Add(catchupFutureAllowance)) || now.Sub(start) > time.Duration(channel.CatchupDays)*24*time.Hour {
		return StreamVariant{}, errf(400, "This programme is outside the channel's replay window")
	}
	if variantIndex < 0 || variantIndex >= len(channel.Variants) {
		variantIndex = 0
	}
	template := channel.CatchupSource
	startUnix := strconv.FormatInt(start.UTC().Unix(), 10)
	endUnix := strconv.FormatInt(stop.UTC().Unix(), 10)
	duration := strconv.FormatInt(int64(stop.Sub(start).Seconds()), 10)
	replacer := strings.NewReplacer(
		"${start}", startUnix, "$start", startUnix, "{utc}", startUnix,
		"${end}", endUnix, "$end", endUnix, "{utcend}", endUnix,
		"${duration}", duration, "$duration", duration,
	)
	resource, err := parseResourceRequest(replacer.Replace(template), nil)
	if err != nil || resource.URL == nil {
		return StreamVariant{}, errf(502, "The channel's catch-up URL is invalid")
	}
	headers := map[string]string{}
	mergeHeaders(headers, channel.Variants[variantIndex].HTTPHeaders)
	mergeHeaders(headers, channel.CatchupHeaders)
	mergeHeaders(headers, resource.HTTPHeaders)
	return StreamVariant{URL: resource.URL.String(), Label: channel.Variants[variantIndex].Label, HTTPHeaders: headers}, nil
}
