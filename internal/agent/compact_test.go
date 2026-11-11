package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

func TestEffectiveMaxTokens_CapsAgainstRemainingRoom(t *testing.T) {
	a := New(Config{
		Client:       &stubClient{},
		Tools:        NewRegistry(),
		ContextLimit: 32768,
		MaxTokens:    32768,
	})
	// prompt already used 30000 of 32768 — only ~2512 actually free.
	got := a.effectiveMaxTokens(30000)
	if got >= 32768 {
		t.Fatalf("effectiveMaxTokens = %d, want it capped well below MaxTokens", got)
	}
	if got < 256 {
		t.Fatalf("effectiveMaxTokens = %d, want at least the floor of 256", got)
	}
}

func TestEffectiveMaxTokens_NoLimitKnown_FallsBackToConfig(t *testing.T) {
	a := New(Config{Client: &stubClient{}, Tools: NewRegistry(), MaxTokens: 4096})
	if got := a.effectiveMaxTokens(30000); got != 4096 {
		t.Fatalf("effectiveMaxTokens = %d, want 4096 (ContextLimit unset)", got)
	}
}

func TestEffectiveMaxTokens_FirstCall_StillCapsAgainstContext(t *testing.T) {
	// ContextLimit known but no prompt measured yet (first call) — must
	// not blindly return the full ceiling if the ceiling is close to or
	// equal to the context size, since the unmeasured prompt isn't zero.
	a := New(Config{
		Client:       &stubClient{},
		Tools:        NewRegistry(),
		ContextLimit: 32768,
		MaxTokens:    32768,
	})
	got := a.effectiveMaxTokens(0)
	if got >= 32768 {
		t.Fatalf("effectiveMaxTokens = %d, want it capped below the full ceiling on an unmeasured first call", got)
	}
}

func TestAgent_CompactHistoryAt90Percent(t *testing.T) {
	client := &usageClient{
		usages:       []int{950, 960},
		finalContent: "done",
	}
	a := New(Config{
		Client:           client,
		Tools:            NewRegistry(echoToolStub{}),
		System:           "sys",
		ContextLimit:     1000,
		CompactKeepSteps: 2,
	})

	// Seed history with many prior steps so compaction has something to drop.
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
	}
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("c%d", i)
		history = append(history,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "echo", Arguments: json.RawMessage(`{}`)}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: "ok"},
		)
	}

	if _, err := a.Resume(context.Background(), history, "continue"); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	found := false
	for _, m := range client.lastHistory {
		if rest, ok := strings.CutPrefix(m.Content, "[harness] "); ok && strings.HasPrefix(rest, prompts.CompactNotice) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected compact notice after crossing 90% context usage")
	}
}

func TestAutoCompactIncludesNewestToolOutput(t *testing.T) {
	h := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "task"}}
	for i := 0; i < 5; i++ {
		h = append(h, llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: fmt.Sprint(i), Name: "large_read", Arguments: json.RawMessage(`{}`)}}}, llm.Message{Role: llm.RoleTool, ToolCallID: fmt.Sprint(i), Content: strings.Repeat("old ", 1000)})
	}
	c := &repeatSeqClient{responses: []llm.ChatResponse{stepOf(llm.ToolCall{ID: "new", Name: "large_read", Arguments: json.RawMessage(`{}`)}), say("done")}}
	c.responses[0].Usage.PromptTokens = 6000
	path := filepath.Join(t.TempDir(), "state.json")
	a := New(Config{Client: c, Tools: NewRegistry(largeRead{}), StateFile: path, ContextLimit: 10000, CompactKeepSteps: 2})
	if _, err := a.Resume(t.Context(), h, "continue"); err != nil {
		t.Fatal(err)
	}
	if len(c.reqs) != 2 {
		t.Fatalf("calls=%d", len(c.reqs))
	}
	for _, m := range c.reqs[1].Messages {
		if m.ToolCallID == "0" {
			t.Fatal("sent old history after the newest result crossed the threshold")
		}
	}
	saved, err := conversation.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ContextStart == 0 || len(saved.History) != len(h)+4 {
		t.Fatalf("auto-compaction lost original messages: archive=%d start=%d", len(saved.History), saved.ContextStart)
	}
	if !reflect.DeepEqual(saved.History[1:len(h)], h[1:]) {
		t.Fatal("auto-compaction rewrote the original archive")
	}
}

func TestFirstRequestReservesOutputAgainstItsActualContent(t *testing.T) {
	c := &repeatSeqClient{responses: []llm.ChatResponse{say("done")}}
	a := New(Config{Client: c, Tools: NewRegistry(), System: strings.Repeat("word ", 4800), ContextLimit: 8192, MaxTokens: 8192})
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	// 24k ASCII characters already account for roughly 6k tokens; the old
	// fixed 1536-token guess requested 6400 output tokens and overfilled 8192.
	if c.reqs[0].MaxTokens > 8192-6000-256 {
		t.Fatalf("output allowance ignores the 24k system prompt: %d", c.reqs[0].MaxTokens)
	}
}

// Compaction drops the results the repeat caches cite: after it fires,
// a cached repeat must re-execute (its bytes are gone from history),
// not refuse with "re-read it from the Identity".
func TestMaybeCompact_LeavesHistoryAloneBelowThreshold(t *testing.T) {
	a := compactAgent(t, 60)
	h := historyWithSteps(10)
	before := len(h)
	if a.maybeCompact(&h, llm.Usage{PromptTokens: 500}, &runState{}) {
		t.Error("compacted at 50% with a 60% threshold")
	}
	if len(h) != before {
		t.Errorf("history changed anyway: %d -> %d", before, len(h))
	}
}

func TestMaybeCompact_FiresAtTheThreshold(t *testing.T) {
	a := compactAgent(t, 60)
	h := historyWithSteps(10)
	before := len(h)
	if !a.maybeCompact(&h, llm.Usage{PromptTokens: 600}, &runState{}) {
		t.Fatal("did not compact at the threshold")
	}
	if len(h) >= before {
		t.Errorf("history not reduced: %d -> %d", before, len(h))
	}
}

// A long run that compacted once and then grew again is in exactly the
// state compaction exists for. The old implementation compacted at most
// once per run and left everything after that unprotected.
func TestMaybeCompact_CompactsAgainAfterHistoryGrows(t *testing.T) {
	a := compactAgent(t, 60)
	st := &runState{}
	h := historyWithSteps(10)
	if !a.maybeCompact(&h, llm.Usage{PromptTokens: 700}, st) {
		t.Fatal("first compaction did not fire")
	}
	// The run keeps going, and the prompt grows with it. Growth is what buys
	// the next pass: without it, a second pass over an unchanged prompt frees
	// nothing the first did not already take, and costs a full re-read.
	h = append(h, historyWithSteps(10)[2:]...)
	if !a.maybeCompact(&h, llm.Usage{PromptTokens: 700 + a.compactionCooldown()}, st) {
		t.Fatal("second compaction did not fire - a long run stays unprotected")
	}
}

// Compacting an already-minimal history every step would spend work and
// drop nothing.
func TestMaybeCompact_StopsWhenThereIsNothingLeftToDrop(t *testing.T) {
	a := compactAgent(t, 60)
	st := &runState{}
	h := historyWithSteps(10)
	if !a.maybeCompact(&h, llm.Usage{PromptTokens: 900}, st) {
		t.Fatal("first compaction did not fire")
	}
	if a.maybeCompact(&h, llm.Usage{PromptTokens: 900}, st) {
		t.Error("compacted again with nothing left to drop")
	}
}
