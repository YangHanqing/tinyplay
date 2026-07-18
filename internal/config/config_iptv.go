package config

// IPTV per-server persistence: favorites and the recently-watched list. The
// RecentChannel type and the IPTVFavorites/IPTVRecent Server fields live in
// config.go with the rest of the schema.

// ToggleIPTVFavorite adds/removes channelID from a server's favorites and
// returns the updated list.
func ToggleIPTVFavorite(serverID, channelID string) []string {
	var result []string
	patch(func(cfg *Config) {
		for _, s := range cfg.Servers {
			if s.ID != serverID {
				continue
			}
			idx := -1
			for i, id := range s.IPTVFavorites {
				if id == channelID {
					idx = i
					break
				}
			}
			if idx >= 0 {
				s.IPTVFavorites = append(s.IPTVFavorites[:idx], s.IPTVFavorites[idx+1:]...)
			} else {
				s.IPTVFavorites = append(s.IPTVFavorites, channelID)
			}
			result = s.IPTVFavorites
			break
		}
	})
	return result
}

// IPTVFavorites returns a server's favorite channel ids.
func IPTVFavorites(serverID string) []string {
	if s := GetServer(serverID); s != nil {
		return s.IPTVFavorites
	}
	return nil
}

// RecordIPTVRecent pushes a channel watch to the front of a server's
// recently-watched list, deduplicating and capping it at limit entries.
func RecordIPTVRecent(serverID, channelID string, limit int) []RecentChannel {
	var result []RecentChannel
	patch(func(cfg *Config) {
		for _, s := range cfg.Servers {
			if s.ID != serverID {
				continue
			}
			entry := RecentChannel{ChannelID: channelID, WatchedAt: nowUTC()}
			kept := make([]RecentChannel, 0, len(s.IPTVRecent)+1)
			kept = append(kept, entry)
			for _, r := range s.IPTVRecent {
				if r.ChannelID != channelID {
					kept = append(kept, r)
				}
			}
			if limit > 0 && len(kept) > limit {
				kept = kept[:limit]
			}
			s.IPTVRecent = kept
			result = s.IPTVRecent
			break
		}
	})
	return result
}

// IPTVRecent returns a server's recently-watched channels, most recent first.
func IPTVRecent(serverID string) []RecentChannel {
	if s := GetServer(serverID); s != nil {
		return s.IPTVRecent
	}
	return nil
}
