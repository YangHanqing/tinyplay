package server

import (
	"encoding/json"
	"testing"

	"tvremote/internal/provider"
)

// fakeMedia implements provider.Media; only ItemDetailRaw is exercised by
// normalizeSearchItems (to fetch a parent Series for a matched episode).
type fakeMedia struct {
	details map[string]string // id -> raw ItemDetail JSON
}

func (f fakeMedia) Kind() string                                         { return "emby" }
func (f fakeMedia) Libraries() ([]byte, error)                           { return nil, nil }
func (f fakeMedia) Items(string, string, int, int, bool) ([]byte, error) { return nil, nil }
func (f fakeMedia) Resume(int, int) ([]byte, error)                      { return nil, nil }
func (f fakeMedia) ItemDetailRaw(id string) ([]byte, error) {
	if raw, ok := f.details[id]; ok {
		return []byte(raw), nil
	}
	return nil, provider.Errorf(404, "not found")
}
func (f fakeMedia) Episodes(string, string, int, int, string) ([]byte, error) { return nil, nil }
func (f fakeMedia) Seasons(string) ([]byte, error)                            { return nil, nil }
func (f fakeMedia) ImageBytes(string, int, string) ([]byte, string)           { return nil, "" }
func (f fakeMedia) BackdropBytes(string, int, int) ([]byte, string)           { return nil, "" }
func (f fakeMedia) ChoosePlayURL(string, string) (provider.PlayChoice, error) {
	return provider.PlayChoice{}, nil
}
func (f fakeMedia) ResumePositionSeconds(string) float64               { return 0 }
func (f fakeMedia) ReportStart(string, string, string)                 {}
func (f fakeMedia) ReportProgress(string, string, int64, bool, string) {}
func (f fakeMedia) ReportStopped(string, string, int64, string)        {}

func decodeItems(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var payload struct {
		Items            []map[string]any `json:"Items"`
		TotalRecordCount int              `json:"TotalRecordCount"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if payload.TotalRecordCount != len(payload.Items) {
		t.Errorf("TotalRecordCount %d != len(Items) %d", payload.TotalRecordCount, len(payload.Items))
	}
	return payload.Items
}

func itemType(m map[string]any) string { s, _ := m["Type"].(string); return s }
func itemID(m map[string]any) string   { s, _ := m["Id"].(string); return s }

// A bare episode whose SeriesName matches collapses into a single Series card
// (the episode itself is dropped, the parent Series is fetched and added).
func TestNormalizeSearch_SeriesTitleMatchCollapsesToSeries(t *testing.T) {
	c := fakeMedia{details: map[string]string{
		"S1": `{"Id":"S1","Name":"Breaking Bad","Type":"Series"}`,
	}}
	in := `{"Items":[{"Id":"E1","Type":"Episode","Name":"Pilot","SeriesId":"S1","SeriesName":"Breaking Bad"}],"TotalRecordCount":1}`
	items := decodeItems(t, normalizeSearchItems(c, []byte(in), "breaking"))
	if len(items) != 1 || itemType(items[0]) != "Series" || itemID(items[0]) != "S1" {
		t.Fatalf("expected one Series S1, got %+v", items)
	}
}

// When the episode's own title matches, the episode is kept (not collapsed).
func TestNormalizeSearch_EpisodeTitleMatchKeepsEpisode(t *testing.T) {
	c := fakeMedia{}
	in := `{"Items":[{"Id":"E1","Type":"Episode","Name":"Pilot","SeriesId":"S1","SeriesName":"Breaking Bad"}],"TotalRecordCount":1}`
	items := decodeItems(t, normalizeSearchItems(c, []byte(in), "pilot"))
	if len(items) != 1 || itemType(items[0]) != "Episode" || itemID(items[0]) != "E1" {
		t.Fatalf("expected the matching Episode E1, got %+v", items)
	}
}

// Raw provider type strings are canonicalized, and a series already present is
// not re-fetched or duplicated.
func TestNormalizeSearch_CanonicalizesTypesAndDedups(t *testing.T) {
	c := fakeMedia{}
	in := `{"Items":[
		{"Id":"M1","Type":"movie","Name":"Breaking Point"},
		{"Id":"S1","Type":"show","Name":"Breaking Bad"},
		{"Id":"S1","Type":"series","Name":"Breaking Bad"}
	],"TotalRecordCount":3}`
	items := decodeItems(t, normalizeSearchItems(c, []byte(in), "breaking"))
	types := map[string]string{}
	for _, it := range items {
		types[itemID(it)] = itemType(it)
	}
	if types["M1"] != "Movie" {
		t.Errorf("movie not canonicalized: %q", types["M1"])
	}
	if types["S1"] != "Series" {
		t.Errorf("show/series not canonicalized: %q", types["S1"])
	}
	// S1 appeared twice in the input; dedup should leave exactly one.
	count := 0
	for _, it := range items {
		if itemID(it) == "S1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected S1 deduped to 1, got %d", count)
	}
}
