package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
)

type browserTestClient func(llm.ChatRequest) llm.ChatResponse

func (f browserTestClient) Chat(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	return f(req), nil
}

func fakeBrowser(connects, actions *int) *Browser {
	return &Browser{connect: func(context.Context, func(string)) ([]agent.Tool, *MCPClient, error) {
		*connects++
		tool := &MCPTool{server: "browser", toolName: "mcp__browser__browser_navigate", def: MCPToolDef{Name: "browser_navigate", InputSchema: json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"],"additionalProperties":false}`)}, client: contractMCP(func(_ context.Context, _ string, _ any, out any) error {
			*actions++
			return json.Unmarshal([]byte(`{"content":[{"type":"text","text":"page loaded"}]}`), out)
		})}
		return []agent.Tool{tool}, nil, nil
	}}
}

func browserRegistry(browser *Browser) (*agent.Registry, *EnableBrowser) {
	tool := &EnableBrowser{Browser: browser}
	reg := agent.NewRegistry(tool)
	tool.Registry = reg
	return reg, tool
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
		name, args := "enable_browser", `{}`
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
	a := agent.New(agent.Config{Client: client, Tools: reg, RestoreTools: enable.Restore, System: "stable system", StateFile: state})
	if _, err := a.Run(t.Context(), "check page"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Run(t.Context(), "enable_browser", json.RawMessage(`{}`)); err != nil || connects != 1 {
		t.Fatal("activation not idempotent", err, connects)
	}

	saved, err := session.Load(state)
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
		if !reflect.DeepEqual(expanded, req.Tools) {
			t.Fatal("resume lost browser schemas")
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}
	})
	a = agent.New(agent.Config{Client: resume, Tools: reopenedReg, RestoreTools: reopenedEnable.Restore, System: "stable system", StateFile: state})
	if _, err := a.Resume(t.Context(), saved.Context(), "continue"); err != nil {
		t.Fatal(err)
	}
	if connects != 2 || actions != 1 {
		t.Fatal("resume replayed an action", connects, actions)
	}
	newReg, _ := browserRegistry(reopened)
	if len(newReg.Defs()) != 1 {
		t.Fatal("activation leaked into a new session")
	}
}

func TestBrowserActivationFailuresDoNotEnableTools(t *testing.T) {
	connects, actions := 0, 0
	browser := fakeBrowser(&connects, &actions)
	reg, enable := browserRegistry(browser)
	failed := true
	archive := []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "activate", Name: "enable_browser", Arguments: json.RawMessage(`{}`)}}}}
	if err := enable.Restore(t.Context(), archive); err != nil || connects != 0 {
		t.Fatal("interrupted activation was replayed", err)
	}
	archive = append(archive, llm.Message{Role: llm.RoleTool, ToolCallID: "activate", ToolFailed: &failed})
	if err := enable.Restore(t.Context(), archive); err != nil || connects != 0 {
		t.Fatal("failed activation was replayed", err)
	}
	browser.connect = func(context.Context, func(string)) ([]agent.Tool, *MCPClient, error) {
		return nil, nil, errors.New("Node missing")
	}
	if _, err := reg.Run(t.Context(), "enable_browser", json.RawMessage(`{}`)); err == nil || len(reg.Defs()) != 1 {
		t.Fatal("failed setup enabled tools")
	}
	if _, err := reg.Run(t.Context(), "enable_browser", json.RawMessage(`{"url":"unexpected"}`)); err == nil {
		t.Fatal("unknown argument accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := reg.Run(ctx, "enable_browser", json.RawMessage(`{}`)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestBrowserCatalogCannotReplaceExistingTools(t *testing.T) {
	connects, actions := 0, 0
	browser := fakeBrowser(&connects, &actions)
	original := browser.connect
	browser.connect = func(ctx context.Context, note func(string)) ([]agent.Tool, *MCPClient, error) {
		list, client, err := original(ctx, note)
		list = append(list, &MCPTool{server: "browser", toolName: "enable_browser", def: MCPToolDef{Name: "browser_bad"}})
		return list, client, err
	}
	reg, enable := browserRegistry(browser)
	if _, err := reg.Run(t.Context(), "enable_browser", json.RawMessage(`{}`)); err == nil {
		t.Fatal("catalog replaced an existing tool")
	}
	if len(reg.Defs()) != 1 || len(enable.Names()) != 0 {
		t.Fatal("part of the invalid catalog was installed")
	}
}
