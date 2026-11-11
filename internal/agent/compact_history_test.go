package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

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
	if !strings.Contains(prompts.CompactNotice, "latest user instructions") {
		t.Fatal("the notice must preserve current user instructions over the mechanical summary")
	}
}

// The plan must survive compaction. It lives only in the last checklist
// call's arguments -- rehydrateTodo rebuilds the tracker by replaying that
// one call -- so dropping it does not lose steps, it loses the model's own
// task list. The notice used to say "todo×2" and nothing else.
func TestCompactHistory_CarriesThePlan(t *testing.T) {
	plan := func(states ...string) []llm.Message {
		texts := []string{"write the grid", "add sliding animation", "handle win state"}
		items := make([]string, 0, len(states))
		for i, st := range states {
			items = append(items, fmt.Sprintf(`{"id":%d,"state":%q,"text":%q}`, i+1, st, texts[i]))
		}
		return []llm.Message{
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
				ID: "t", Name: "todo",
				Arguments: json.RawMessage(`{"items":[` + strings.Join(items, ",") + `]}`),
			}}},
			{Role: llm.RoleTool, ToolCallID: "t", Content: "ok"},
		}
	}
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "build me a sliding puzzle"},
	}
	history = append(history, plan("done", "pending", "pending")...)
	for i := 0; i < 6; i++ {
		history = append(history, assistantStep("a")...)
	}
	// The most recent checklist is the plan as it stood when compaction hit.
	history = append(history, plan("done", "in_progress", "pending")...)
	for i := 0; i < 6; i++ {
		history = append(history, assistantStep("b")...)
	}

	notice := compactHistory(history, 4)[2].Content
	for _, want := range []string{"[x] write the grid", "[~] add sliding animation", "[ ] handle win state"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("plan item %q missing from the notice:\n%s", want, notice)
		}
	}
	if !strings.Contains(prompts.CompactNotice, "checklist may be stale") {
		t.Fatal("the notice must not present an extracted checklist as authoritative")
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
