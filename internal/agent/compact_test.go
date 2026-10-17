package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/session"
)

func assistantStep(id string) []llm.Message {
	return []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "echo"}}},
		{Role: llm.RoleTool, ToolCallID: id, Content: "ok"},
	}
}

// CompactFile compacts a snapshot in place and reports counts; a
// second call is a no-op that writes nothing.
func TestCompactFile_ShrinksSnapshot(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state.json")
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
	}
	for i := 0; i < 15; i++ {
		history = append(history, assistantStep("c")...)
	}
	raw, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	before, after, err := CompactFile(state, 8)
	if err != nil {
		t.Fatalf("CompactFile: %v", err)
	}
	if after >= before {
		t.Fatalf("before=%d after=%d, want shrink", before, after)
	}
	loaded, err := LoadState(state)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(loaded) != after {
		t.Fatalf("snapshot holds %d messages, want %d", len(loaded), after)
	}

	before2, after2, err := CompactFile(state, 8)
	if err != nil {
		t.Fatalf("second CompactFile: %v", err)
	}
	if before2 != after || after2 != after {
		t.Fatalf("second call (%d→%d) must be a no-op", before2, after2)
	}
}

func TestCompactFile_MissingSnapshotErrors(t *testing.T) {
	if _, _, err := CompactFile(filepath.Join(t.TempDir(), "missing.json"), 8); err == nil {
		t.Fatal("want error for missing snapshot")
	}
}

// /compact reads and rewrites one snapshot. A corrupt file is an error,
// not something to reconstruct: there is no second copy to fall back to,
// and writing a partial reconstruction would look like a recovered
// session.
func TestCompactFile_CorruptSnapshotErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := session.WriteSnapshot(path, []llm.Message{{Role: llm.RoleUser, Content: "task"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CompactFile(path, 8); err == nil {
		t.Fatal("a corrupt snapshot must not compact silently")
	}
}

func TestCompactFile_ShrinksASnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	history := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "task"}}
	for i := 0; i < 15; i++ {
		history = append(history, assistantStep("c")...)
	}
	if err := session.WriteSnapshot(path, history); err != nil {
		t.Fatal(err)
	}

	before, after, err := CompactFile(path, 8)
	if err != nil || before != len(history) || after >= before {
		t.Fatalf("compaction: %d -> %d, %v", before, after, err)
	}
	loaded, err := LoadState(path)
	if err != nil || len(loaded) != after {
		t.Fatalf("compacted snapshot: %d messages, %v", len(loaded), err)
	}
}

func TestCompactHistory_KeepsPrefixAndRecentGroups(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
	}
	for i := 0; i < 15; i++ {
		history = append(history, assistantStep("c")...)
	}

	got := compactHistory(history, 8)
	if len(got) < 2+1+8*2 {
		t.Fatalf("too short: %d messages", len(got))
	}
	if got[0].Content != "sys" || got[1].Content != "task" {
		t.Fatal("prefix lost")
	}
	if got[2].Content != harnessText(prompts.CompactNotice) && len(got[2].Content) <= len(prompts.CompactNotice) {
		t.Fatalf("expected compact notice at [2], got %q", got[2].Content)
	}
	if want := harnessText(prompts.CompactNotice); len(got[2].Content) < len(want) || got[2].Content[:len(want)] != want {
		t.Fatalf("expected marked compact notice prefix at [2], got %q", got[2].Content)
	}
}

// A session is not one long task. Compaction keeps the FIRST user message
// as the standing instruction, so when an early question was an aside and
// the real work came later, compaction drops the real request and keeps
// the aside. The model then finishes whatever work is still visible in the
// tail, runs out, and answers the aside.
//
// This is a transcript shape: user asks about currency, then asks for a
// game, then asks for polish. Only the currency question survives as live
// instruction; the polish request must reach the model through the notice.
func TestCompactHistory_CarriesDroppedUserRequests(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "show me today's rate for the ruble"},
	}
	for i := 0; i < 6; i++ {
		history = append(history, assistantStep("a")...)
	}
	// The actual work, dropped by compaction.
	history = append(history, llm.Message{Role: llm.RoleUser, Content: "now build me a sliding puzzle game"})
	for i := 0; i < 6; i++ {
		history = append(history, assistantStep("b")...)
	}
	history = append(history, llm.Message{Role: llm.RoleUser, Content: "polish it: add sliding animations to the tiles"})
	for i := 0; i < 4; i++ {
		history = append(history, assistantStep("c")...)
	}

	got := compactHistory(history, 4)
	notice := got[2].Content

	for _, want := range []string{"build me a sliding puzzle game", "add sliding animations to the tiles"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("dropped request %q missing from the notice:\n%s", want, notice)
		}
	}
	// Order matters: the newest request is the live one, and the notice
	// says so. A model that reads them unordered is back to guessing.
	if strings.Index(notice, "add sliding animations") < strings.Index(notice, "build me a sliding puzzle") {
		t.Fatalf("dropped requests must stay in order, newest last:\n%s", notice)
	}
	if !strings.Contains(prompts.CompactNotice, "the one you are working on") {
		t.Fatal("the notice must state which dropped request is current")
	}
}

// Loop notices describe the loop, not the task. Listing them as the user's
// requests would bury the request that matters.
func TestSummarizeDropped_SkipsHarnessNotices(t *testing.T) {
	summary := summarizeDropped([][]llm.Message{{
		{Role: llm.RoleUser, Content: harnessText("budget extended by operator approval")},
		{Role: llm.RoleAssistant, Content: "working"},
		{Role: llm.RoleUser, Content: "actually make it two players"},
	}})
	if strings.Contains(summary, "budget extended") {
		t.Fatalf("harness notice leaked into the dropped requests: %s", summary)
	}
	if !strings.Contains(summary, "actually make it two players") {
		t.Fatalf("real request missing: %s", summary)
	}
}

func TestCompactHistory_NoOrphanToolResults(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
	}
	for i := 0; i < 10; i++ {
		id := "call"
		history = append(history,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: "echo"}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: id, Content: "ok"},
		)
	}

	got := compactHistory(history, 3)
	for i, m := range got {
		if m.Role != llm.RoleTool {
			continue
		}
		if i == 0 || got[i-1].Role != llm.RoleAssistant {
			t.Fatalf("orphan tool result at %d", i)
		}
	}
}

func TestCompactHistory_DisabledWhenKeepStepsZero(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
		{Role: llm.RoleAssistant, Content: "hi"},
	}
	if got := compactHistory(history, 0); len(got) != len(history) {
		t.Fatal("expected unchanged history")
	}
}

func TestStepGroups_SplitsOnAssistant(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleAssistant, Content: "a1"},
		{Role: llm.RoleTool, Content: "t1"},
		{Role: llm.RoleUser, Content: "nudge"},
		{Role: llm.RoleAssistant, Content: "a2"},
	}
	groups := stepGroups(msgs)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if len(groups[0]) != 3 || len(groups[1]) != 1 {
		t.Fatalf("unexpected group sizes: %d, %d", len(groups[0]), len(groups[1]))
	}
}

// Delegate reports name no files in their call arguments — the touched
// paths live in the result envelope. They must survive in the notice,
// or post-compaction history remembers that a subagent ran but not what
// it changed.
func TestSummarizeDropped_KeepsDelegatePaths(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
	}
	for i := 0; i < 10; i++ {
		history = append(history,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c", Name: "delegate_task"}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: "c", Content: "DELEGATE\nmutations: 2\npaths: sub/a.go\npaths: sub/b.go\n----\ndone"},
		)
	}
	got := compactHistory(history, 3)
	if len(got) < 3 {
		t.Fatalf("too short: %d messages", len(got))
	}
	notice := got[2].Content
	if !strings.Contains(notice, "sub/a.go") || !strings.Contains(notice, "sub/b.go") {
		t.Fatalf("delegate paths lost from notice: %q", notice)
	}
}

// history builds a transcript with n assistant-led step groups after the
// system/user prefix, which is the shape compactHistory splits on.
func historyWithSteps(n int) []llm.Message {
	out := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
	}
	for i := 0; i < n; i++ {
		out = append(out,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c", Name: "read_file"}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: "c", Content: "contents"},
		)
	}
	return out
}

func compactAgent(t *testing.T, atPercent int) *Agent {
	t.Helper()
	return New(Config{
		Client: &stubClient{}, Tools: NewRegistry(), System: "sys",
		ContextLimit: 1000, CompactKeepSteps: 2, CompactAtPercent: atPercent,
	})
}

// Compaction drops the results the repeat caches cite: after it fires,
// a cached repeat must re-execute (its bytes are gone from history),
// not refuse with "re-read it from the Identity".
func TestMaybeCompact_ClearsRepeatCaches(t *testing.T) {
	a := compactAgent(t, 60)
	st := &runState{
		idempotentSeen: map[string]int{"read_file|a": 3},
		failedCalls:    map[string]int{"read_file|b": 2},
	}
	h := historyWithSteps(10)
	if !a.maybeCompact(&h, llm.Usage{PromptTokens: 900}, st) {
		t.Fatal("setup: compaction did not fire")
	}
	if len(st.idempotentSeen) != 0 || len(st.failedCalls) != 0 {
		t.Fatalf("repeat caches survived compaction: %+v / %+v", st.idempotentSeen, st.failedCalls)
	}
}

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
	h = append(h, historyWithSteps(10)[2:]...) // the run keeps going
	if !a.maybeCompact(&h, llm.Usage{PromptTokens: 700}, st) {
		t.Fatal("second compaction did not fire — a long run stays unprotected")
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
