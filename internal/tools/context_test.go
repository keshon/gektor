package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

type inspectionClient struct {
	calls []llm.ToolCall
	count int
}

func TestHistorySearchDoesNotMatchItsOwnUnsentCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	client := &inspectionClient{calls: []llm.ToolCall{{ID: "search", Name: "read_history", Arguments: json.RawMessage(`{"query":"only-in-this-search-call"}`)}}}
	var result agent.ToolResult
	a := agent.New(agent.Config{Client: client, Tools: agent.NewRegistry(ReadHistory{Path: path}), System: "system", StateFile: path, OnResult: func(r agent.ToolResult) { result = r }})
	if _, err := a.Run(t.Context(), "Find an absent record"); err != nil {
		t.Fatal(err)
	}
	var out struct{ Messages []json.RawMessage }
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil || result.Failed || len(out.Messages) != 0 {
		t.Fatal("search matched its own query", result, err)
	}
}

func (c *inspectionClient) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	c.count++
	if c.count == 1 {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: c.calls}}, nil
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

type contextActivation struct{ reg *agent.Registry }

func (contextActivation) Name() string            { return "activate_test" }
func (contextActivation) Description() string     { return "Activate a test tool" }
func (contextActivation) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (contextActivation) Mode() agent.ToolMode    { return agent.Exclusive }
func (t contextActivation) Run(context.Context, json.RawMessage) (string, error) {
	return "activated", t.reg.Add(ReadHistory{})
}

func TestInspectContextUsesCallingRequestAndPagesInstructions(t *testing.T) {
	reg := agent.NewRegistry(InspectContext{})
	if err := reg.Add(contextActivation{reg}); err != nil {
		t.Fatal(err)
	}
	client := &inspectionClient{}
	inputs := []struct{ id, name, args string }{
		{"activate", "activate_test", `{}`},
		{"overview", "inspect_context", `{}`},
		{"tools", "inspect_context", `{"view":"tools"}`},
		{"identity", "inspect_context", `{"view":"instructions","section":"identity"}`},
		{"tail", "inspect_context", `{"view":"instructions","section":"identity","offset":2000}`},
		{"revisions", "inspect_context", `{"view":"revisions"}`},
		{"skills", "inspect_context", `{"view":"skills"}`},
		{"bad_revision", "inspect_context", `{"view":"instructions","revision":999}`},
		{"bad_offset", "inspect_context", `{"view":"instructions","offset":99999}`},
		{"bad_view", "inspect_context", `{"view":"unknown"}`},
	}
	for _, in := range inputs {
		client.calls = append(client.calls, llm.ToolCall{ID: in.id, Name: in.name, Arguments: json.RawMessage(in.args)})
	}
	results := map[string]agent.ToolResult{}
	identity := strings.Repeat("Ж", 2100)
	a := agent.New(agent.Config{Client: client, Tools: reg, System: "core\n" + identity, SystemSections: []llm.PromptSection{{Name: "identity", Text: identity}}, OnResult: func(r agent.ToolResult) { results[r.CallID] = r }})
	if _, err := a.Run(t.Context(), "inspect"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"bad_revision", "bad_offset", "bad_view"} {
		if !results[id].Failed {
			t.Fatal("accepted invalid input", id, results[id])
		}
	}
	for _, id := range []string{"overview", "tools", "identity", "tail", "revisions", "skills"} {
		if results[id].Failed || !json.Valid([]byte(results[id].Output)) {
			t.Fatal(id, results[id])
		}
	}
	if strings.Contains(results["tools"].Output, "read_history") || !reg.Has("read_history") {
		t.Fatal("tool list was reconstructed after activation", results["tools"])
	}
	var first, tail struct {
		Text string
		Next int `json:"next_offset"`
	}
	if err := json.Unmarshal([]byte(results["identity"].Output), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(results["tail"].Output), &tail); err != nil {
		t.Fatal(err)
	}
	if first.Next != 2000 || first.Text+tail.Text != identity || tail.Next != 0 {
		t.Fatal("broken Unicode paging", first.Next, tail.Next)
	}
	if _, err := (InspectContext{}).Run(t.Context(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("inspection outside agent request succeeded")
	}
}

func TestInspectOverviewPagesSparseArchiveMembership(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "task"}}
	for range 25 {
		history = append(history, llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "bad", Name: "broken", InputError: "invalid"}}}, llm.Message{Role: llm.RoleUser, Content: "retained"})
	}
	client := &inspectionClient{calls: []llm.ToolCall{{ID: "first", Name: "inspect_context", Arguments: json.RawMessage(`{}`)}, {ID: "tail", Name: "inspect_context", Arguments: json.RawMessage(`{"offset":20}`)}}}
	results := map[string]agent.ToolResult{}
	a := agent.New(agent.Config{Client: client, Tools: agent.NewRegistry(InspectContext{}), System: "system", OnResult: func(r agent.ToolResult) { results[r.CallID] = r }})
	if _, err := a.Resume(t.Context(), history, ""); err != nil {
		t.Fatal(err)
	}
	var first, tail struct {
		Manifest conversation.RequestManifest
		Next     int `json:"next_offset"`
	}
	if err := json.Unmarshal([]byte(results["first"].Output), &first); err != nil {
		t.Fatal(results, err)
	}
	if err := json.Unmarshal([]byte(results["tail"].Output), &tail); err != nil {
		t.Fatal(results, err)
	}
	if len(first.Manifest.Included) != 20 || first.Next != 20 || len(tail.Manifest.Included) != 6 || tail.Next != 0 {
		t.Fatal(first, tail)
	}
}
