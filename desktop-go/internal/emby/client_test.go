package emby

import "testing"

func videoSource(fields map[string]any) map[string]any {
	stream := map[string]any{"Type": "Video"}
	for k, v := range fields {
		stream[k] = v
	}
	return map[string]any{"MediaStreams": []any{stream}}
}

func TestSourceIsDVProfile5FromExplicitProfile(t *testing.T) {
	source := videoSource(map[string]any{"DvProfile": float64(5)})
	if !sourceIsDVProfile5(source) {
		t.Fatal("expected explicit DvProfile=5 to be detected")
	}
}

func TestSourceIsDVProfile5FromStringProfileOnSource(t *testing.T) {
	source := map[string]any{
		"DvProfile": "5",
		"MediaStreams": []any{
			map[string]any{"Type": "Video"},
		},
	}
	if !sourceIsDVProfile5(source) {
		t.Fatal("expected string DvProfile=5 on media source to be detected")
	}
}

func TestSourceIsDVProfile5FromCodecTag(t *testing.T) {
	source := videoSource(map[string]any{"CodecTag": "dvhe.05.06"})
	if !sourceIsDVProfile5(source) {
		t.Fatal("expected dvhe.05 codec tag to be detected")
	}
}

func TestSourceIsDVProfile5FromDoviOnlyRange(t *testing.T) {
	source := videoSource(map[string]any{"VideoRangeType": "DOVI"})
	if !sourceIsDVProfile5(source) {
		t.Fatal("expected DOVI-only range to be treated as profile 5 fallback material")
	}
}

func TestSourceIsDVProfile5DoesNotMatchHDR10CompatibleProfiles(t *testing.T) {
	cases := []map[string]any{
		{"DvProfile": float64(8)},
		{"CodecTag": "dvhe.08.06", "VideoRangeType": "DOVIWithHDR10"},
		{"DisplayTitle": "4K HEVC Dolby Vision Profile 7 / HDR10"},
	}
	for _, fields := range cases {
		if sourceIsDVProfile5(videoSource(fields)) {
			t.Fatalf("did not expect profile 5 detection for %#v", fields)
		}
	}
}
