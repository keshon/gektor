package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

func TestCheckpointsRecoverEvidenceAcrossCompactionAndResume(t *testing.T) {
	const fabricated = `{"goal":"old goal","decisions":[],"completed":["all tests pass"],"remaining":["blocked on credentials"],"evidence":["invented proof"]}`
	c := &repeatSeqClient{}
	for range 5 {
		c.responses = append(c.responses, say(fabricated))
	}
	h := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "Build server"}}
	a := New(Config{Client: c, Tools: NewRegistry(), ContextLimit: 8192, MaxTokens: 2048})
	a.snapshot = session.New(h)
	a.checkpointLen = len(h)
	path := filepath.Join(t.TempDir(), "state.json")
	failed, succeeded := true, false
	for pass := range 5 {
		checkID, editID := fmt.Sprintf("check-%d", pass), fmt.Sprintf("edit-%d", pass)
		requirement := fmt.Sprintf("Change %d: use SQLite instead of Postgres\nDo not publish", pass)
		h = append(h,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: checkID, Name: "run_program", Arguments: json.RawMessage(`{"program":"go","args":["test","./..."]}`)}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: checkID, ToolFailed: &failed, Content: fmt.Sprintf("FAIL unique-diagnostic-%d", pass)},
			llm.Message{Role: llm.RoleUser, Content: requirement},
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: editID, Name: "write_file", Arguments: json.RawMessage(`{"path":"server.go"}`)}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: editID, ToolFailed: &succeeded, Content: "changed after check"},
			llm.Message{Role: llm.RoleAssistant, Content: "tail"},
		)
		a.captureHistory(h)
		next := compactHistory(h, 1)
		var err error
		next, err = retainArchivedInstructions(a.snapshot, next)
		if err != nil {
			t.Fatal(err)
		}
		next, err = a.checkpoint(t.Context(), h, next)
		if err != nil {
			t.Fatal(err)
		}
		facts, ok := parseFacts(strings.TrimPrefix(next[2].Content, harnessPrefix))
		if !ok || !containsStr(facts.asks, requirement) || strings.Contains(facts.checkpoint, "invented proof") || !strings.Contains(facts.checkpoint, "blocked on credentials") {
			t.Fatal("requirements, blocker or evidence corrupted", facts)
		}
		var summary struct{ Evidence []string }
		if err := json.Unmarshal([]byte(facts.checkpoint), &summary); err != nil {
			t.Fatal(err)
		}
		if len(summary.Evidence) > 6 || len(summary.Evidence) < 2 {
			t.Fatal(summary)
		}
		last := summary.Evidence[len(summary.Evidence)-2:]
		if !strings.Contains(last[0], "execution=failed") || !strings.Contains(last[1], "write_file") || !strings.Contains(last[1], "execution=succeeded") {
			t.Fatal("lost failure or edit order", last)
		}
		for _, entry := range summary.Evidence {
			var id int
			if _, err := fmt.Sscanf(entry, "archive #%d:", &id); err != nil {
				t.Fatal(entry, err)
			}
			rows, err := session.PageHistory(a.snapshot, id, 1, 0)
			if err != nil || rows[0].Role != llm.RoleTool || !strings.Contains(entry, rows[0].Text) {
				t.Fatal("invalid evidence reference", entry, rows, err)
			}
		}
		if pass > 0 {
			wantID := len(a.snapshot.History) - 4 // current failed result, one-based
			if !strings.Contains(c.reqs[pass].Messages[1].Content, fmt.Sprintf("Archive message %d (tool", wantID)) {
				t.Fatal("transcript used projected IDs", c.reqs[pass].Messages[1].Content)
			}
		}
		a.compactContext(h, next)
		if err := a.snapshot.Save(path); err != nil {
			t.Fatal(err)
		}
		a.snapshot, err = session.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		h = a.snapshot.Context()
		a.checkpointLen = len(h)
	}
	// Even evidence evicted from the six-result ledger remains recoverable.
	rows, _, err := session.SearchHistory(a.snapshot, "unique-diagnostic-0", 1, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != 4 || rows[0].Failed == nil || !*rows[0].Failed {
		t.Fatal(rows, err)
	}
}

func TestCheckpointEvidenceNeverInfersUnknownExecutionSuccess(t *testing.T) {
	evidence := checkpointEvidence([]llm.Message{{Role: llm.RoleTool, Content: "PASS"}})
	if len(evidence) != 1 || !strings.Contains(evidence[0], "execution=unknown") {
		t.Fatal(evidence)
	}
}

// Opt-in semantic smoke test against the operator's local model. Deterministic
// tests above establish harness guarantees; this checks one model's summaries.
func TestCheckpointContinuityLive(t *testing.T) {
	endpoint := os.Getenv("GEKTOR_CHECKPOINT_LIVE_URL")
	if endpoint == "" {
		t.Skip("set GEKTOR_CHECKPOINT_LIVE_URL to a local llama.cpp endpoint")
	}
	a := New(Config{Client: llm.NewLlamaClient(endpoint, "local"), Tools: NewRegistry(), ContextLimit: 16384, MaxTokens: 1536})
	h := []llm.Message{{Role: llm.RoleSystem, Content: "Coding assistant"}, {Role: llm.RoleUser, Content: "Build a Postgres server"}}
	a.snapshot, a.checkpointLen = session.New(h), len(h)
	failed := true
	for pass, target := range []string{"SQLite", "JSON file"} {
		id := fmt.Sprintf("verify-%d", pass)
		h = append(h,
			llm.Message{Role: llm.RoleUser, Content: "Replace the earlier storage requirement: use " + target + ". Do not publish. Credentials are unavailable; deployment is blocked."},
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "run_program", Arguments: json.RawMessage(`{"program":"go","args":["test","./..."]}`)}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: id, ToolFailed: &failed, Content: "FAIL server_test.go: expected 200, got 500"},
			llm.Message{Role: llm.RoleAssistant, Content: "Tests passed and deployment is complete."},
			llm.Message{Role: llm.RoleAssistant, Content: "continue"},
		)
		a.captureHistory(h)
		next, err := retainArchivedInstructions(a.snapshot, compactHistory(h, 1))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
		next, err = a.checkpoint(ctx, h, next)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		facts, _ := parseFacts(strings.TrimPrefix(next[2].Content, harnessPrefix))
		var summary struct {
			Goal                 string
			Decisions, Remaining []string
		}
		if err := json.Unmarshal([]byte(facts.checkpoint), &summary); err != nil {
			t.Fatal(err)
		}
		current := strings.ToLower(summary.Goal + " " + strings.Join(summary.Decisions, " "))
		remaining := strings.ToLower(strings.Join(summary.Remaining, " "))
		if !strings.Contains(current, strings.ToLower(target)) || !strings.Contains(remaining, "test") || !strings.Contains(remaining, "credential") {
			t.Fatalf("lost changed goal, failed check or blocker: %s", facts.checkpoint)
		}
		t.Logf("pass=%d checkpoint=%s", pass+1, facts.checkpoint)
		a.compactContext(h, next)
		h = next
	}
}
