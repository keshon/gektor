package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

// A dropped plan is bounded: this string is carried for the rest of the
// run, so a 40-item checklist cannot turn the notice into the largest
// thing in the prompt.
func TestFormatChecklist_BoundsThePlan(t *testing.T) {
	items := make([]string, 0, planMaxItems+10)
	for i := 0; i < planMaxItems+10; i++ {
		items = append(items, fmt.Sprintf(`{"id":%d,"state":"pending","text":"item number %d"}`, i, i))
	}
	got, ok := formatChecklist(json.RawMessage(`{"items":[` + strings.Join(items, ",") + `]}`))
	if !ok {
		t.Fatal("a well-formed checklist must be recognised")
	}
	if !strings.Contains(got, "+10 more") {
		t.Fatalf("overflow not announced: %s", got)
	}
	if n := strings.Count(got, ";"); n != planMaxItems {
		t.Fatalf("%d items rendered, want %d: %s", n+1, planMaxItems, got)
	}
}

// Arguments that merely happen to have an items array are not a checklist.
func TestFormatChecklist_RejectsOtherTools(t *testing.T) {
	for _, args := range []string{
		`{}`,
		`{"items":[]}`,
		`{"items":[{"id":1,"state":"unknown","text":"x"}]}`,
		`{"items":[{"id":1,"state":"pending","text":"  "}]}`,
		`{"items":[{"id":1}]}`,
		`not json`,
	} {
		if _, ok := formatChecklist(json.RawMessage(args)); ok {
			t.Fatalf("%s must not be read as a checklist", args)
		}
	}
}

// Loop notices describe the loop, not the task. Listing them as the user's
// requests would bury the request that matters.
func TestSummarizeDropped_SkipsHarnessNotices(t *testing.T) {
	var f droppedFacts
	f.add([][]llm.Message{{
		{Role: llm.RoleUser, Content: harnessText("budget extended by operator approval")},
		{Role: llm.RoleAssistant, Content: "working"},
		{Role: llm.RoleUser, Content: "actually make it two players"},
	}})
	summary := f.render()
	if strings.Contains(summary, "budget extended") {
		t.Fatalf("harness notice leaked into the dropped requests: %s", summary)
	}
	if !strings.Contains(summary, "actually make it two players") {
		t.Fatalf("real request missing: %s", summary)
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
