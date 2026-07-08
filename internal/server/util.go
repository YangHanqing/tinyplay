package server

import (
	"io/fs"

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

func readWeb(s *Server, name string) ([]byte, error) {
	return fs.ReadFile(s.webFS, name)
}
