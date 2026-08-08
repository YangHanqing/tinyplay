package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordTitleSettingsDirtyMergeAndLRU(t *testing.T) {
	useTempData(t)
	// First play: only speed was dirty.
	speed := 1.5
	RecordTitleSettings("srv-a", "item-1", TitleSettingsPatch{Speed: &speed})
	entry, ok := LookupTitleSettings("srv-a", "item-1")
	if !ok || entry.Speed == nil || *entry.Speed != 1.5 {
		t.Fatalf("speed-only record: %+v ok=%v", entry, ok)
	}
	if entry.Subtitle != nil || entry.Audio != nil {
		t.Fatalf("untouched fields must not be stored: %+v", entry)
	}

	// Second play of the same title: only subtitle dirty. Speed must survive.
	sub := TrackChoice{Lang: "jpn", Title: "Japanese"}
	RecordTitleSettings("srv-a", "item-1", TitleSettingsPatch{Subtitle: &sub})
	entry, ok = LookupTitleSettings("srv-a", "item-1")
	if !ok {
		t.Fatal("entry disappeared after merge")
	}
	if entry.Speed == nil || *entry.Speed != 1.5 {
		t.Fatalf("merge must keep prior speed: %+v", entry)
	}
	if entry.Subtitle == nil || entry.Subtitle.Lang != "jpn" {
		t.Fatalf("merge must write subtitle: %+v", entry)
	}

	// Empty patch is a no-op (an untouched playback must not freeze defaults).
	before := len(Load().TitleSettingsHistory)
	RecordTitleSettings("srv-a", "item-1", TitleSettingsPatch{})
	if len(Load().TitleSettingsHistory) != before {
		t.Fatal("empty patch should not rewrite history")
	}
}

func TestTitleSettingsExcludedSourcesNotStoredByCaller(t *testing.T) {
	// Eligibility lives in the player package; config still accepts any key.
	// This test pins that DeleteServer / ResetAll clean the table so a record
	// never outlives its source.
	useTempData(t)
	speed := 2.0
	srv := AddServer(Server{Name: "NAS", Type: "emby", Hosts: []string{"nas"}, Port: 8096})
	RecordTitleSettings(srv.ID, "movie-1", TitleSettingsPatch{Speed: &speed})
	if _, ok := LookupTitleSettings(srv.ID, "movie-1"); !ok {
		t.Fatal("expected stored entry")
	}
	if !DeleteServer(srv.ID) {
		t.Fatal("delete failed")
	}
	if _, ok := LookupTitleSettings(srv.ID, "movie-1"); ok {
		t.Fatal("title settings must be removed with their source")
	}
}

func TestTitleSettingsResetAllClearsHistory(t *testing.T) {
	useTempData(t)
	speed := 1.25
	RecordTitleSettings("srv", "path/a.mkv", TitleSettingsPatch{Speed: &speed})
	SetKeepPlaybackSpeed(false)
	SetRememberTitleSettings(true)
	ResetAll()
	cfg := Load()
	if len(cfg.TitleSettingsHistory) != 0 {
		t.Fatalf("history after reset: %+v", cfg.TitleSettingsHistory)
	}
	if !cfg.KeepPlaybackSpeed {
		t.Fatal("KeepPlaybackSpeed should reset to true")
	}
	if cfg.RememberTitleSettings {
		t.Fatal("RememberTitleSettings should reset to false")
	}
}

func TestTitleSettingsSettingsAPIFields(t *testing.T) {
	useTempData(t)
	s := Settings()
	// Defaults: keep on, remember off.
	if s["keep_playback_speed"] != true {
		t.Fatalf("keep default = %v", s["keep_playback_speed"])
	}
	if s["remember_title_settings"] != false {
		t.Fatalf("remember default = %v", s["remember_title_settings"])
	}
	SetKeepPlaybackSpeed(false)
	SetRememberTitleSettings(true)
	s = Settings()
	if s["keep_playback_speed"] != false || s["remember_title_settings"] != true {
		t.Fatalf("after set: keep=%v remember=%v", s["keep_playback_speed"], s["remember_title_settings"])
	}
}

func TestTitleSettingsDefaultsOnMissingJSON(t *testing.T) {
	dir := useTempData(t)
	// Old configs lack the new fields; defaults must still apply on load.
	raw := []byte(`{"servers":[],"listen_port":1980,"autoplay_next_episode":true}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Load()
	if !cfg.KeepPlaybackSpeed {
		t.Fatal("missing keep_playback_speed must default true")
	}
	if cfg.RememberTitleSettings {
		t.Fatal("missing remember_title_settings must default false")
	}
}

func TestTitleSettingsHistoryNotSerializedWhenEmpty(t *testing.T) {
	useTempData(t)
	// Touch unrelated settings so a save happens.
	SetKeepPlaybackSpeed(true)
	raw, err := os.ReadFile(filepath.Join(DataDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["title_settings_history"]; ok {
		t.Fatal("empty history should be omitempty")
	}
}
