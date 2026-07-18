package player

import "testing"

func TestDiagnosticsFreezeLatestAttempt(t *testing.T) {
	p := &Player{liveProps: map[string]any{"time-pos": 12.5, "pause": false}}
	p.beginDiagnostic("https://media.example.test/stream", PlayOptions{SourceType: "emby"})
	p.recordMPVEvent("file-loaded", map[string]any{})
	p.recordMPVEvent("end-file", map[string]any{"reason": "error", "error": "loading failed"})
	p.finalizeDiagnostic("engine_process_exit", "engine_process")

	report, ok := p.Diagnostics()
	if !ok {
		t.Fatal("expected frozen diagnostic report")
	}
	if got := report["report_scope"]; got != "last" {
		t.Fatalf("scope = %v, want last", got)
	}
	attempt := report["playback_attempt"].(map[string]any)
	if got := attempt["outcome"]; got != "failed" {
		t.Fatalf("outcome = %v, want failed", got)
	}
	if got := attempt["termination_reason"]; got != "backend_error" {
		t.Fatalf("termination_reason = %v, want backend_error", got)
	}
	engine := report["engine"].(map[string]any)
	if got := engine["end_file_reason"]; got != "error" {
		t.Fatalf("end_file_reason = %v, want error", got)
	}
}

// The other diagnostic tests call recordMPVEvent directly, which cannot catch a
// wire-format regression. mpv puts end-file's fields on the event object itself
// ({"event":"end-file","reason":"eof"}), so decode the same bytes mpv sends.
func TestHandleEventExtractsEndFileFieldsFromMPVWireFormat(t *testing.T) {
	p := &Player{liveProps: map[string]any{}}
	p.beginDiagnostic("https://media.example.test/stream", PlayOptions{SourceType: "emby"})
	p.handleEvent([]byte(`{"event":"end-file","reason":"eof","playlist_entry_id":1}` + "\n"))

	p.diagMu.Lock()
	reason := p.currentDiagnostic.MPVEndReason
	p.diagMu.Unlock()
	if reason != "eof" {
		t.Fatalf("MPVEndReason = %q, want eof (natural EOF drives autoplay)", reason)
	}
}

// property-change keeps its payload under "data" — that field belongs to the
// event, so it must not be confused with the flattened top-level fields above.
func TestHandleEventStillReadsPropertyChangeData(t *testing.T) {
	p := &Player{liveProps: map[string]any{}}
	p.handleEvent([]byte(`{"event":"property-change","id":1,"name":"time-pos","data":42.5}` + "\n"))

	p.propsMu.Lock()
	got := p.liveProps["time-pos"]
	p.propsMu.Unlock()
	if got != 42.5 {
		t.Fatalf("time-pos = %v, want 42.5", got)
	}
}

func TestDiagnosticsUsesCompletedOutcomeForEOF(t *testing.T) {
	p := &Player{liveProps: map[string]any{}}
	p.beginDiagnostic("http://127.0.0.1:1980/api/files/stream", PlayOptions{SourceType: "smb"})
	p.recordMPVEvent("end-file", map[string]any{"reason": "eof"})
	p.finalizeDiagnostic("engine_process_exit", "engine_process")

	report, ok := p.Diagnostics()
	if !ok {
		t.Fatal("expected frozen diagnostic report")
	}
	attempt := report["playback_attempt"].(map[string]any)
	if got := attempt["outcome"]; got != "completed" {
		t.Fatalf("outcome = %v, want completed", got)
	}
	if got := attempt["termination_reason"]; got != "completed" {
		t.Fatalf("termination_reason = %v, want completed", got)
	}
}
