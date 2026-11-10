package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
)

type bucketProgressClient struct{}

func (bucketProgressClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	for _, n := range []int{0, 1024, 2048} {
		req.OnProgress(llm.Progress{Phase: "prefill", ProcessedTokens: n})
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

func TestSessionDiagnosticsKeepsPrefillBuckets(t *testing.T) {
	dir := t.TempDir()
	s, err := New(Config{Env: testEnv(t, bucketProgressClient{}, nil), Task: "task", StateFile: filepath.Join(dir, "state.json")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "diagnostics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["event"] == "progress" && record["phase"] == "prefill" {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("prefill buckets=%d, want 3", n)
	}
}

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

func TestDiagnosticsRetainsCacheCountsWithoutPerTokenWrites(t *testing.T) {
	dir := t.TempDir()
	s := &Session{cfg: Config{StateFile: filepath.Join(dir, "state.json")}}
	closeLog := s.diagnostics()
	for i := range 3000 {
		s.cfg.OnEvent(events.Event{Name: "progress", Fields: map[string]any{
			"phase": "prefill", "request": 5, "prompt": 12000,
			"cached": 8000, "cache_known": true, "processed": 8000 + i,
			"completion": 0, "estimated": false, "detail": "SECRET",
		}})
	}
	for i := range 1000 {
		s.cfg.OnEvent(events.Event{Name: "progress", Fields: map[string]any{
			"phase": "generating", "request": 5, "prompt": 12000,
			"cached": 8000, "cache_known": true, "processed": 12000,
			"completion": i, "estimated": false,
		}})
	}
	s.cfg.OnEvent(events.Event{Name: "usage", Fields: map[string]any{
		"step": 4, "prompt": 12000, "completion": 1000, "cached": 8000, "estimated": false,
	}})
	closeLog()
	raw, err := os.ReadFile(filepath.Join(dir, "diagnostics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) > 9 || strings.Contains(string(raw), "SECRET") {
		t.Fatal("unbounded or sensitive diagnostics", string(raw))
	}
	var usage map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-2]), &usage); err != nil {
		t.Fatal(err)
	}
	if usage["event"] != "usage" || usage["cached"] != float64(8000) || usage["completion"] != float64(1000) {
		t.Fatal(usage)
	}
	if !strings.Contains(string(raw), `"cache_known":true`) || !strings.Contains(string(raw), `"processed":`) {
		t.Fatal("missing cache telemetry", string(raw))
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
