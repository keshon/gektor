package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
)

// A compaction notice is a user-role message, so stepGroups counted it as a
// kept step. Past the threshold every pass re-crossed the keep guard,
// dropped the notice to make room, and rebuilt the summary from a window
// that no longer held any of the older work: the window drained one group
// per step instead of one per keepSteps, and the plan was gone from the
// second pass on.
//
// Both tests below drive several successive steps, which is the only shape
// that catches it. The single-pass tests elsewhere pass either way.

func planHistory(groups int) []llm.Message {
	h := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "build the thing"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c0", Name: "todo",
			Arguments: json.RawMessage(`{"items":[{"id":"1","state":"pending","text":"write the parser"}]}`)}}},
		{Role: llm.RoleTool, ToolCallID: "c0", Content: "ok"},
	}
	for i := 1; i < groups; i++ {
		h = append(h,
			llm.Message{Role: llm.RoleAssistant, Content: "step " + string(rune('a'+i%26))},
			llm.Message{Role: llm.RoleUser, Content: "also handle " + string(rune('a'+i%26))},
		)
	}
	return h
}

func firstNotice(t *testing.T, h []llm.Message) string {
	t.Helper()
	for _, m := range h {
		if strings.HasPrefix(m.Content, harnessPrefix) {
			return m.Content
		}
	}
	t.Fatal("no harness notice in history")
	return ""
}

func TestCompactHistory_KeepsTheNoticeOutOfTheKeepWindow(t *testing.T) {
	const keep = 8
	h := planHistory(12)

	for step := 0; step < 12; step++ {
		h = append(h, llm.Message{Role: llm.RoleAssistant, Content: "more " + string(rune('A'+step))})
		h = compactHistory(h, keep)

		_, rest := splitNotice(h[2:])
		if len(rest) == 0 {
			t.Fatalf("step %d: the notice was consumed instead of carried", step)
		}
		if got := len(stepGroups(rest)); got != keep {
			t.Fatalf("step %d: kept %d groups of work, want %d", step, got, keep)
		}
		if !strings.HasPrefix(h[2].Content, harnessPrefix) {
			t.Fatalf("step %d: notice left index 2", step)
		}
	}
}

func TestCompactHistory_CarriesTheNoticeAcrossPasses(t *testing.T) {
	const keep = 8
	h := planHistory(12)
	before := firstNotice(t, compactHistory(h, keep))

	h = compactHistory(h, keep)
	for step := 0; step < 6; step++ {
		h = append(h, llm.Message{Role: llm.RoleAssistant, Content: "more " + string(rune('A'+step))})
		h = compactHistory(h, keep)
	}
	after := firstNotice(t, h)

	for _, want := range []string{"write the parser", "also handle"} {
		if !strings.Contains(before, want) {
			t.Fatalf("first pass never carried %q: %s", want, before)
		}
		if !strings.Contains(after, want) {
			t.Fatalf("%q lost after further passes: %s", want, after)
		}
	}
}

// The rendered block is the only record of the older work once the messages
// are gone, so it has to survive its own round trip.
func TestDroppedFacts_RoundTripsThroughItsOwnRendering(t *testing.T) {
	var f droppedFacts
	f.add([][]llm.Message{{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "write_file", Arguments: json.RawMessage(`{"path":"a.go"}`)},
			{ID: "2", Name: "grep_files", Arguments: json.RawMessage(`{}`)},
		}},
		{Role: llm.RoleTool, ToolCallID: "1", Content: "ok"},
		{Role: llm.RoleUser, Content: "also make it two players"},
	}})
	f.plan = "[ ] write the parser"
	f.lastText = `he said "done"`

	got, ok := parseFacts(f.render())
	if !ok {
		t.Fatalf("rendered block did not parse back:\n%s", f.render())
	}
	if got.steps != f.steps || got.plan != f.plan || got.lastText != f.lastText {
		t.Fatalf("scalar fields lost: %+v", got)
	}
	if got.tools["write_file"] != 1 || got.tools["grep_files"] != 1 {
		t.Fatalf("tool counts lost: %v", got.tools)
	}
	if len(got.paths) != 1 || got.paths[0] != "a.go" {
		t.Fatalf("paths lost: %v", got.paths)
	}
	if len(got.asks) != 1 || got.asks[0] != "also make it two players" {
		t.Fatalf("asks lost: %v", got.asks)
	}
}

// Stuck, budget and think-wrap notices are also user-role and carry the same
// prefix. Lifting one of those would delete the advice and import nothing.
func TestSplitNotice_LeavesOtherHarnessNoticesAlone(t *testing.T) {
	for _, notice := range []string{
		prompts.OverflowRecovered,
		prompts.LeakDetected,
		prompts.Truncated,
	} {
		rest := []llm.Message{
			{Role: llm.RoleUser, Content: harnessText(notice)},
			{Role: llm.RoleAssistant, Content: "working"},
		}
		carried, kept := splitNotice(rest)
		if len(kept) != len(rest) {
			t.Fatalf("%q was lifted as a compaction notice", notice)
		}
		if carried.steps != 0 {
			t.Fatalf("%q was parsed as facts: %+v", notice, carried)
		}
	}
}
