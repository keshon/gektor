package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/session"
	"github.com/keshon/gektor/internal/tools"
)

type extensionLookup struct{ calls *int }

type extensionInspectClient func(llm.ChatRequest) llm.ChatResponse

func (f extensionInspectClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	return f(req), nil
}

func (extensionLookup) Name() string { return "cap__fixture__lookup" }
func (extensionLookup) Description() string {
	return "Read the fixture value for a key: alpha, beta or gamma."
}
func (extensionLookup) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","enum":["alpha","beta","gamma"]}},"required":["key"],"additionalProperties":false}`)
}
func (extensionLookup) Mode() agent.ToolMode { return agent.Concurrent }
func (l extensionLookup) Run(_ context.Context, args json.RawMessage) (string, error) {
	*l.calls++
	var in struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	return map[string]string{"alpha": "alpha-714", "beta": "beta-928", "gamma": "gamma-563"}[in.Key], nil
}

func fixtureCapability(loads, calls *int, unavailable bool) *tools.Capability {
	return &tools.Capability{ID: "fixture", Version: "1", Description: "Look up fixture values", Source: "fixture-v1", ReadOnly: true, Concurrent: true, ResourceField: "key", Instructions: "Use cap__fixture__lookup to retrieve values. Never guess.", Load: func(context.Context, func(string)) ([]agent.Tool, error) {
		*loads++
		if unavailable {
			return nil, fmt.Errorf("fixture service unavailable")
		}
		return []agent.Tool{extensionLookup{calls: calls}}, nil
	}}
}

func extensionCall(id, name, args string) llm.ChatResponse {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: name, Arguments: json.RawMessage(args)}}}}
}
func extensionAnswer() llm.ChatResponse {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}
}

func TestExtensionResumeCompactionAndCurrentPermissions(t *testing.T) {
	e := testEnv(t)
	loads, calls := 0, 0
	e.Capabilities = []*tools.Capability{fixtureCapability(&loads, &calls, false)}
	state := filepath.Join(t.TempDir(), "state.json")
	e.Client = &seqStub{steps: []llm.ChatResponse{extensionCall("enable", "enable_capability", `{"id":"fixture"}`), extensionCall("lookup", "cap__fixture__lookup", `{"key":"alpha"}`), extensionAnswer()}}
	if _, err := Interactive(e, "", state, nil, nil).Run(t.Context(), "Look up alpha"); err != nil {
		t.Fatal(err)
	}
	saved, err := session.Load(state)
	if err != nil || len(saved.Capabilities) != 1 || calls != 1 {
		t.Fatal(saved.Capabilities, err, calls)
	}
	if _, _, _, err := agent.CompactFile(state, 1); err != nil {
		t.Fatal(err)
	}
	saved, err = session.Load(state)
	if err != nil || len(saved.Capabilities) != 1 {
		t.Fatal(err)
	}
	// An unrelated resumed answer must not load an unavailable adapter.
	e.Capabilities = []*tools.Capability{fixtureCapability(&loads, &calls, true)}
	e.Client = extensionInspectClient(func(req llm.ChatRequest) llm.ChatResponse {
		found := false
		for _, message := range req.Messages {
			if strings.Contains(message.Content, "Use cap__fixture__lookup to retrieve values. Never guess.") {
				found = true
			}
		}
		if !found {
			t.Error("compaction lost capability instructions")
		}
		return extensionAnswer()
	})
	if _, err := Interactive(e, "", state, nil, nil).Resume(t.Context(), saved.Context(), "Explain the task"); err != nil || loads != 1 {
		t.Fatal("resume connected optional adapter", err, loads)
	}
	for _, deny := range []bool{true, false} {
		saved, err = session.Load(state)
		if err != nil {
			t.Fatal(err)
		}
		e.Capabilities = []*tools.Capability{fixtureCapability(&loads, &calls, false)}
		e.Policy = permission.Default()
		if deny {
			e.Policy = e.Policy.WithExtra([]permission.Rule{{Tool: "cap__fixture__*", Pattern: "beta", Effect: permission.Deny}})
		}
		e.Client = &seqStub{steps: []llm.ChatResponse{extensionCall("beta", "cap__fixture__lookup", `{"key":"beta"}`), extensionAnswer()}}
		failed := false
		e.OnResult = func(result agent.ToolResult) { failed = result.Failed }
		before := calls
		if _, err := Planner(e, "", state, nil).Resume(t.Context(), saved.Context(), "Read beta in Plan"); err != nil {
			t.Fatal(err)
		}
		if deny && (calls != before || !failed) {
			t.Fatal("current policy bypassed", calls, failed)
		}
		if !deny && (calls != before+1 || failed) {
			t.Fatal("trusted read-only extension blocked in Plan", calls, failed)
		}
	}
}

func TestUnavailableLegacyExtensionDoesNotAbortCodingConversation(t *testing.T) {
	e := testEnv(t)
	loads, calls := 0, 0
	e.Capabilities = []*tools.Capability{fixtureCapability(&loads, &calls, true)}
	e.Client = &seqStub{steps: []llm.ChatResponse{extensionAnswer()}}
	failed := false
	history := []llm.Message{{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "task"}, extensionCall("enable", "enable_capability", `{"id":"fixture"}`).Message, {Role: llm.RoleTool, ToolCallID: "enable", Content: "enabled", ToolFailed: &failed}, extensionAnswer().Message}
	state := filepath.Join(t.TempDir(), "state.json")
	if err := session.WriteSnapshot(state, history); err != nil {
		t.Fatal(err)
	}
	warned := false
	e.OnNudge = func(kind, text string) {
		if strings.Contains(text, "Optional session tools unavailable") {
			warned = true
		}
	}
	if _, err := Interactive(e, "", state, nil, nil).Resume(t.Context(), history, "Continue explaining the code"); err != nil || !warned || loads != 1 {
		t.Fatal(err, warned, loads)
	}
}

func writeFixtureSkill(t *testing.T, library string) {
	t.Helper()
	dir := filepath.Join(library, "fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	guide := "---\nname: fixture\ndescription: Look up fixture values using the optional fixture capability.\n---\nDiscover and enable the fixture capability if lookup tools are absent. Retrieve the requested key through cap__fixture__lookup. Prefix a successful final value with RESULT:. If retrieval fails, answer UNAVAILABLE and never guess."
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(guide), 0600); err != nil {
		t.Fatal(err)
	}
}
