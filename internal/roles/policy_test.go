package roles

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
)

func TestProcessKillCommandsRequireApprovalInBothExecutionModes(t *testing.T) {
	e := testEnv(t)
	var calls []llm.ToolCall
	for i, c := range []struct{ name, input string }{
		{"run_shell", `{"command":"taskkill /IM GEKTOR_TEST_NOT_REAL.exe /F"}`},
		{"start_background", `{"command":"taskkill /IM GEKTOR_TEST_NOT_REAL.exe /F"}`},
		{"run_program", `{"program":"taskkill","args":["/IM","GEKTOR_TEST_NOT_REAL.exe","/F"]}`},
		{"start_program", `{"program":"taskkill","args":["/IM","GEKTOR_TEST_NOT_REAL.exe","/F"]}`},
	} {
		calls = append(calls, llm.ToolCall{ID: c.name + string(rune('0'+i)), Name: c.name, Arguments: json.RawMessage(c.input)})
	}
	e.Client = &seqStub{steps: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: calls}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "Blocked."}},
	}}
	asked, blocked := 0, 0
	e.Gate = func(_ context.Context, tool, resource string, _ json.RawMessage) (permission.Effect, error) {
		asked++
		if !strings.Contains(resource, "taskkill") {
			t.Fatalf("command arguments absent from permission resource: %s", resource)
		}
		return permission.Deny, nil
	}
	e.OnResult = func(result agent.ToolResult) {
		if result.Failed && strings.Contains(result.Output, "blocked by policy") {
			blocked++
		}
	}
	if _, err := Interactive(e, "", "", nil, nil).Run(t.Context(), "test process ownership"); err != nil {
		t.Fatal(err)
	}
	if asked != 4 || blocked != 4 {
		t.Fatalf("asked=%d blocked=%d; expected four blocked calls", asked, blocked)
	}
}

func TestPlannerRejectsGitMutationsBeforeExecution(t *testing.T) {
	e := testEnv(t)
	e.Client = &seqStub{steps: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "add", Name: "git_add", Arguments: json.RawMessage(`{"path":"."}`)},
			{ID: "commit", Name: "git_commit", Arguments: json.RawMessage(`{"message":"not allowed"}`)},
		}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "Plan only."}},
	}}
	var results []agent.ToolResult
	e.OnResult = func(r agent.ToolResult) { results = append(results, r) }
	a := Planner(e, "planner", "", nil)
	for _, name := range []string{"git_add", "git_commit"} {
		if !hasTool(a, name) {
			t.Fatalf("%s missing from the common schema; switching mode would change the prefix", name)
		}
	}
	if _, err := a.Run(t.Context(), "Inspect the project"); err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected two refusals, got %+v", results)
	}
	for _, r := range results {
		if !r.Failed || !strings.Contains(r.Output, "read-only") {
			t.Fatalf("mutation reached execution instead of the read-only guard: %+v", r)
		}
	}
}
