package run

import (
	"github.com/keshon/gektor/internal/events"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticsPersistsBoundariesWithoutContent(t *testing.T) {
	dir := t.TempDir()
	seen := 0
	s := &Session{cfg: Config{StateFile: filepath.Join(dir, "state.json"), OnEvent: func(events.Event) { seen++ }}}
	closeLog := s.diagnostics()
	s.cfg.OnEvent(events.Event{Name: "progress", Fields: map[string]any{"phase": "waiting", "subphase": "headers", "request": 2, "detail": "SECRET"}})
	s.cfg.OnEvent(events.Event{Name: "step", Fields: map[string]any{"text": "SECRET", "tool_calls": []any{map[string]any{"id": "call1", "name": "enable_browser", "args": "SECRET"}}}})
	s.cfg.OnEvent(events.Event{Name: "tool_result", Fields: map[string]any{"call_id": "call1", "failed": false, "text": "SECRET"}})
	closeLog()
	raw, err := os.ReadFile(filepath.Join(dir, "diagnostics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "SECRET") || seen != 3 {
		t.Fatal(text, seen)
	}
	for _, want := range []string{"run_start", "run_end", "elapsed_ms", "headers", "enable_browser", "call1", "failed"} {
		if !strings.Contains(text, want) {
			t.Fatal("missing", want, text)
		}
	}
	s.cfg.OnEvent(events.Event{})
	if seen != 4 {
		t.Fatal("sink not restored")
	}
}

func TestDiagnosticsRotatesAndCoalescesProgress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "diagnostics.jsonl")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 2<<20)), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Session{cfg: Config{StateFile: filepath.Join(dir, "state.json")}}
	closeLog := s.diagnostics()
	for range 100 {
		s.cfg.OnEvent(events.Event{Name: "progress", Fields: map[string]any{"phase": "prefill", "request": 1}})
	}
	closeLog()
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), `"event":"progress"`) != 1 {
		t.Fatal(string(raw))
	}
}
