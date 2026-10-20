package agent

import (
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

// The guard exists so a kept group never opens with an orphaned tool
// result. A lone user turn is not an orphan: it is the user asking for
// something, and it is exactly what compaction must not silently refuse
// to drop.
func TestFindCutPoint_NeverOrphansToolResult(t *testing.T) {
	toolLed := [][]llm.Message{
		{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "1"}}}},
		{{Role: llm.RoleTool, ToolCallID: "1", Content: "ok"}},
		{{Role: llm.RoleAssistant, Content: "b"}},
	}
	if got := findCutPoint(toolLed, 1); got != 0 {
		t.Fatalf("cut = %d, want 0: kept[0] would orphan a tool result", got)
	}
	if got := findCutPoint(toolLed, 2); got != 2 {
		t.Fatalf("clean cut = %d, want 2", got)
	}

	userLed := [][]llm.Message{
		{{Role: llm.RoleAssistant, Content: "a"}},
		{{Role: llm.RoleUser, Content: "actually make it two players"}},
		{{Role: llm.RoleAssistant, Content: "b"}},
	}
	// Backing off from here is what made a session with interleaved user
	// messages uncompactable: the cut walked to 0 and nothing was dropped.
	if got := findCutPoint(userLed, 1); got != 1 {
		t.Fatalf("cut = %d, want 1: a user turn is not an orphan", got)
	}
}

func TestSummarizeDropped_ListsToolsAndFiles(t *testing.T) {
	dropped := [][]llm.Message{{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "write_file", Arguments: []byte(`{"path":"a.go"}`)},
			{ID: "2", Name: "grep_files", Arguments: []byte(`{}`)},
		}},
		{Role: llm.RoleTool, ToolCallID: "1", Content: "ok"},
	}}
	var f droppedFacts
	f.add(dropped)
	s := f.render()
	if !strings.Contains(s, "write_file") || !strings.Contains(s, "a.go") {
		t.Fatalf("summary missing facts: %q", s)
	}
}

func TestCompactNotice_StartsWithBaseNotice(t *testing.T) {
	h := historyWithSteps(10)
	got := compactHistory(h, 2)
	if len(got) < 3 {
		t.Fatalf("too short: %d", len(got))
	}
	if !strings.HasPrefix(got[2].Content, harnessText(prompts.CompactNotice)) {
		t.Fatalf("marked notice prefix lost: %q", got[2].Content)
	}
	if !strings.Contains(got[2].Content, "dropped") {
		t.Fatalf("extractive summary missing: %q", got[2].Content)
	}
}
