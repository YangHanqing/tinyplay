package server

import (
	"io/fs"
	"net/url"
	"strconv"

	"tvremote/internal/player"
)

func playOpts(serverID, itemID, seriesID, seasonID, title, seriesTitle, episodeLabel, posterItemID string, startSeconds float64, mediaSourceID string) player.PlayOptions {
	return player.PlayOptions{
		ServerID:      serverID,
		ItemID:        itemID,
		SeriesID:      seriesID,
		SeasonID:      seasonID,
		Title:         title,
		SeriesTitle:   seriesTitle,
		EpisodeLabel:  episodeLabel,
		PosterItemID:  posterItemID,
		StartSeconds:  startSeconds,
		MediaSourceID: mediaSourceID,
	}
}

// fileStreamPlayURL builds the loopback streaming-proxy URL used for every
// file-source play (manual handler and host autoplay). Kept in one place so
// the two call sites cannot drift on query shape or host/port.
func (s *Server) fileStreamPlayURL(serverID, path string) string {
	return "http://127.0.0.1:" + strconv.Itoa(s.port) +
		"/api/files/stream?server_id=" + url.QueryEscape(serverID) +
		"&path=" + url.QueryEscape(path)
}

func readWeb(s *Server, name string) ([]byte, error) {
	return fs.ReadFile(s.webFS, name)
}
