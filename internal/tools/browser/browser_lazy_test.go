package browser

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
)

type browserTestClient func(llm.ChatRequest) llm.ChatResponse

func (f browserTestClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	return f(req), nil
}

func fakeBrowser(connects, actions *int) *Browser {
	return &Browser{connect: func(context.Context, func(string)) ([]agent.Tool, browserSession, error) {
		*connects++
		tool := &browserFixture{name: "mcp__browser__browser_navigate", remote: "browser_navigate", schema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`), run: func(json.RawMessage) (string, error) {
			*actions++
			return "page loaded", nil
		}}
		return []agent.Tool{tool}, nil, nil
	}}
}

func browserRegistry(browser *Browser) (*agent.Registry, *toolextension.Capabilities) {
	reg := agent.NewRegistry()
	catalog := &toolextension.Capabilities{Registry: reg, Sources: []*toolextension.Capability{browser.Capability()}}
	if err := reg.Add(toolextension.EnableCapability{Catalog: catalog}); err != nil {
		panic(err)
	}
	return reg, catalog
}

func TestBrowserLoadsOnceAndRestoresAfterCompaction(t *testing.T) {
	connects, actions := 0, 0
	browser := fakeBrowser(&connects, &actions)
	defer browser.Close()
	reg, enable := browserRegistry(browser)
	state := filepath.Join(t.TempDir(), "state.json")
	var expanded []llm.ToolDef
	step := 0
	client := browserTestClient(func(req llm.ChatRequest) llm.ChatResponse {
		if req.Messages[0].Content != "stable system" {
			t.Fatal("activation rewrote system instructions")
		}
		step++
		name, args := "enable_capability", `{"id":"browser"}`
		switch step {
		case 1:
			if len(req.Tools) != 1 || connects != 0 {
				t.Fatal("browser eagerly loaded")
			}
		case 2:
			if len(req.Tools) != 2 || connects != 1 || !strings.Contains(req.Messages[len(req.Messages)-1].Content, browserInstructions) {
				t.Fatal("activation missing from model context")
			}
			expanded = req.Tools
			name, args = "mcp__browser__browser_navigate", `{"url":"http://localhost"}`
		case 3:
			if !reflect.DeepEqual(expanded, req.Tools) || actions != 1 {
				t.Fatal("tool set changed after activation")
			}
			return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}
		default:
			t.Fatal("unexpected model request")
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: name, Name: name, Arguments: json.RawMessage(args)}}}, FinishReason: "tool_calls"}
	})
	a := agent.New(agent.Config{Client: client, Tools: reg, Catalog: enable, System: "stable system", StateFile: state})
	if _, err := a.Run(t.Context(), "check page"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Run(t.Context(), "enable_capability", json.RawMessage(`{"id":"browser"}`)); err != nil || connects != 1 {
		t.Fatal("activation not idempotent", err, connects)
	}

	saved, err := conversation.Load(state)
	if err != nil {
		t.Fatal(err)
	}
	projection := append([]llm.Message(nil), saved.History[:2]...)
	projection = append(projection, llm.Message{Role: llm.RoleUser, Content: "checkpoint"}, saved.History[len(saved.History)-1])
	saved.Compact(projection)
	if err := saved.Save(state); err != nil {
		t.Fatal(err)
	}
	// Reopen with a new transport and a fresh registry. The compacted context
	// contains neither activation nor its successful result.
	reopened := fakeBrowser(&connects, &actions)
	defer reopened.Close()
	reopenedReg, reopenedEnable := browserRegistry(reopened)
	resume := browserTestClient(func(req llm.ChatRequest) llm.ChatResponse {
		want, _ := json.Marshal(expanded)
		got, _ := json.Marshal(req.Tools)
		if string(want) != string(got) {
			t.Fatal("resume lost browser schemas")
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}
	})
	a = agent.New(agent.Config{Client: resume, Tools: reopenedReg, Catalog: reopenedEnable, System: "stable system", StateFile: state})
	if _, err := a.Resume(t.Context(), saved.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	if connects != 1 || actions != 1 {
		t.Fatal("catalog restore reconnected or replayed an action", connects, actions)
	}
	newReg, _ := browserRegistry(reopened)
	if len(newReg.Defs()) != 1 {
		t.Fatal("activation leaked into a new session")
	}
}

func TestBrowserActivationFailuresDoNotEnableTools(t *testing.T) {
	connects, actions := 0, 0
	browser := fakeBrowser(&connects, &actions)
	reg, _ := browserRegistry(browser)

	browser.connect = func(context.Context, func(string)) ([]agent.Tool, browserSession, error) {
		return nil, nil, errors.New("Node missing")
	}
	if _, err := reg.Run(t.Context(), "enable_capability", json.RawMessage(`{"id":"browser"}`)); err == nil || len(reg.Defs()) != 1 {
		t.Fatal("failed setup enabled tools")
	}
	if _, err := reg.Run(t.Context(), "enable_capability", json.RawMessage(`{"url":"unexpected"}`)); err == nil {
		t.Fatal("unknown argument accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reg.Run(ctx, "enable_capability", json.RawMessage(`{"id":"browser"}`)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBrowserCatalogCannotReplaceExistingTools(t *testing.T) {
	connects, actions := 0, 0
	browser := fakeBrowser(&connects, &actions)
	original := browser.connect
	browser.connect = func(ctx context.Context, note func(string)) ([]agent.Tool, browserSession, error) {
		list, client, err := original(ctx, note)
		list = append(list, &browserFixture{name: "enable_capability", remote: "browser_bad"})
		return list, client, err
	}
	reg, enable := browserRegistry(browser)
	if _, err := reg.Run(t.Context(), "enable_capability", json.RawMessage(`{"id":"browser"}`)); err == nil {
		t.Fatal("catalog replaced an existing tool")
	}
	if len(reg.Defs()) != 1 || len(enable.Save()) != 0 {
		t.Fatal("part of the invalid catalog was installed")
	}
}

// browserFixture exercises activation at the registry boundary.
type browserFixture struct {
	name, remote string
	schema       json.RawMessage
	run          func(json.RawMessage) (string, error)
}

func (t *browserFixture) Name() string        { return t.name }
func (t *browserFixture) RemoteName() string  { return t.remote }
func (t *browserFixture) Description() string { return "fixture" }
func (t *browserFixture) Schema() json.RawMessage {
	if t.schema != nil {
		return t.schema
	}
	return json.RawMessage(`{"type":"object"}`)
}
func (t *browserFixture) Mode() agent.ToolMode { return agent.Exclusive }
func (t *browserFixture) Run(_ context.Context, args json.RawMessage) (string, error) {
	if t.run != nil {
		return t.run(args)
	}
	return "", nil
}
func TestBrowserInstructionBudget(t *testing.T) {
	if len(browserInstructions) > 1024 {
		t.Fatalf("browser instructions grew to %d bytes", len(browserInstructions))
	}
}
