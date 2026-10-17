package session

import (
	"os"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/llm"
)

func TestAppendLoadRoundTrip(t *testing.T) {
	path := LogPath(t.TempDir() + "/state.json")
	h := []llm.Message{{Role: llm.RoleUser, Content: "hi"}}
	Append(path[:len(path)-len(".jsonl")]+".json", 0, h)
	Append(path[:len(path)-len(".jsonl")]+".json", 1, append(h, llm.Message{Role: llm.RoleAssistant, Content: "ok"}))
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 || got[1].Content != "ok" {
		t.Fatalf("unexpected history: %+v", got)
	}
}

// Only the last entry is ever read, and every entry is a full
// checkpoint. Appending one per step without bound wrote the whole
// conversation once per step: a 90-turn session kept 6 MB to resume from
// 46 KB, 99% of it stale the moment the next step landed.
func TestAppend_BoundsTheLog(t *testing.T) {
	state := t.TempDir() + "/state.json"
	for i := 0; i < 40; i++ {
		if err := Append(state, i, []llm.Message{{Role: llm.RoleUser, Content: "turn"}}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	data, err := os.ReadFile(LogPath(state))
	if err != nil {
		t.Fatal(err)
	}
	if n := countLines(string(data)); n != logKeepEntries {
		t.Fatalf("log holds %d entries, want %d", n, logKeepEntries)
	}
	// Trimming must not cost recovery: the newest entry still loads.
	got, err := Load(LogPath(state))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Content != "turn" {
		t.Fatalf("recovered %+v", got)
	}
}

// One entry short of a torn append is what the retained second entry is
// for, so the previous checkpoint must survive the newest one landing.
func TestAppend_KeepsThePreviousCheckpoint(t *testing.T) {
	state := t.TempDir() + "/state.json"
	Append(state, 0, []llm.Message{{Role: llm.RoleUser, Content: "first"}})
	Append(state, 1, []llm.Message{{Role: llm.RoleUser, Content: "second"}})

	data, err := os.ReadFile(LogPath(state))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"first"`) || !strings.Contains(string(data), `"second"`) {
		t.Fatalf("both checkpoints must be retained:\n%s", data)
	}
}

func countLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func TestLoadSkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	state := dir + "/state.json"
	Append(state, 0, []llm.Message{{Role: llm.RoleUser, Content: "a"}})
	f, _ := os.OpenFile(LogPath(state), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("not json\n")
	_ = f.Close()
	got, err := Load(LogPath(state))
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestLoadEmptyFails(t *testing.T) {
	if _, err := Load(t.TempDir() + "/missing.jsonl"); err == nil {
		t.Fatal("expected error")
	}
}

func TestTitleFor_SkipsHarnessAndSystem(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "[harness] verify yourself"},
		{Role: llm.RoleUser, Content: "real task\nsecond line"},
	}
	if got := TitleFor(history); got != "real task" {
		t.Fatalf("TitleFor = %q", got)
	}
	if got := TitleFor(nil); got != "" {
		t.Fatalf("TitleFor(nil) = %q", got)
	}
}
