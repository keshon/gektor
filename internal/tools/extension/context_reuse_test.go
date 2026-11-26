package extension

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	toolskill "github.com/keshon/gektor/internal/tools/skill"
	"github.com/keshon/gektor/internal/workspace"
)

type workflowGuideClient struct {
	calls    []llm.ToolCall
	requests [][]llm.Message
}

func (c *workflowGuideClient) Chat(_ context.Context, request llm.ChatRequest) (llm.ChatResponse, error) {
	c.requests = append(c.requests, request.Messages)
	if len(c.requests) == 1 && len(c.calls) > 0 {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: c.calls}}, nil
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

func TestWorkflowGuideProjectionReusesBothCallOrders(t *testing.T) {
	for _, skillFirst := range []bool{true, false} {
		name := "activation first"
		if skillFirst {
			name = "skill first"
		}
		t.Run(name, func(t *testing.T) {
			ws, err := workspace.NewWithSkillRoots(t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			catalog := &Capabilities{Registry: agent.NewRegistry(), Native: []*NativePackage{{
				ID: "native_workflows", Version: "1", Exposure: Lazy, Instructions: workspace.WorkflowGuide,
				Tools: []agent.Tool{&nativeInterfaceProbe{}},
			}}}
			err = catalog.Registry.Add(EnableCapability{Catalog: catalog}, toolskill.Tool{WS: ws})
			if err != nil {
				t.Fatal(err)
			}
			load := llm.ToolCall{ID: "skill", Name: "load_skill", Arguments: json.RawMessage(`{"id":"workflows"}`)}
			enable := llm.ToolCall{ID: "activation", Name: "enable_capability", Arguments: json.RawMessage(`{"id":"native_workflows"}`)}
			calls := []llm.ToolCall{enable, load}
			if skillFirst {
				calls = []llm.ToolCall{load, enable}
			}
			client := &workflowGuideClient{calls: calls}
			path := filepath.Join(t.TempDir(), "state.json")
			cfg := agent.Config{Client: client, Tools: catalog.Registry, Catalog: catalog, System: "system", StateFile: path}
			_, err = agent.New(cfg).Run(t.Context(), "load workflow guidance")
			if err != nil {
				t.Fatal(err)
			}
			bodies, reminders, projectedBytes := 0, 0, 0
			for _, message := range client.requests[len(client.requests)-1] {
				if message.Role == llm.RoleTool {
					projectedBytes += len(message.Content)
				}
				if strings.Contains(message.Content, workspace.WorkflowGuide) {
					bodies++
				}
				if message.Role == llm.RoleTool && strings.Contains(message.Content, "already present above") {
					reminders++
					if skillFirst && !strings.Contains(message.Content, "native_probe_tool") {
						t.Fatal("activation lost its published tool names")
					}
				}
			}
			if bodies != 1 || reminders != 1 || !catalog.Registry.Has("native_probe_tool") {
				t.Fatalf("guidance or activation changed: bodies=%d reminders=%d", bodies, reminders)
			}
			saved, err := conversation.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			results := make(map[string]llm.Message)
			archivedBytes := 0
			for _, message := range saved.History {
				if message.Role == llm.RoleTool {
					if !strings.HasSuffix(message.Content, workspace.WorkflowGuide) {
						t.Fatal("projection altered durable full guide")
					}
					results[message.ToolCallID] = message
					archivedBytes += len(message.Content)
				}
			}
			t.Logf("tool content: archived=%d bytes projected=%d bytes saved=%d bytes", archivedBytes, projectedBytes, archivedBytes-projectedBytes)
			repeated := &workflowGuideClient{}
			cfg.Client = repeated
			_, err = agent.New(cfg).Resume(t.Context(), saved.Context(), "continue")
			if err != nil {
				t.Fatal(err)
			}
			bodies, reminders = 0, 0
			for _, message := range repeated.requests[0] {
				if strings.Contains(message.Content, workspace.WorkflowGuide) {
					bodies++
				}
				if message.Role == llm.RoleTool && strings.Contains(message.Content, "already present above") {
					reminders++
				}
			}
			if bodies != 1 || reminders != 1 {
				t.Fatalf("repeat request changed reuse: bodies=%d reminders=%d", bodies, reminders)
			}
			// Split the archived calls into separate rounds so actual compaction can
			// remove the first delivery and retain the previously shortened result.
			history := append([]llm.Message(nil), saved.History[:2]...)
			for _, call := range calls {
				history = append(history, llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}, results[call.ID])
			}
			saved = conversation.New(history)
			saved.Capabilities = catalog.Save()
			err = saved.Save(path)
			if err != nil {
				t.Fatal(err)
			}
			_, _, compacted, err := agent.CompactFile(path, 1)
			if err != nil {
				t.Fatal(err)
			}
			resumed := &workflowGuideClient{}
			cfg.Client = resumed
			_, err = agent.New(cfg).Resume(t.Context(), compacted, "continue")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, message := range resumed.requests[0] {
				if message.Role == llm.RoleTool && message.ToolCallID == calls[1].ID {
					found = true
					if !strings.HasSuffix(message.Content, workspace.WorkflowGuide) {
						t.Fatal("compaction left a reminder without a retained full tool guide")
					}
				}
			}
			if !found {
				t.Fatal("compaction did not retain the expected tool result")
			}
		})
	}
}

func TestWorkflowGuideReusePreservesChangedActivationInstructions(t *testing.T) {
	ws, err := workspace.NewWithSkillRoots(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	skill := toolskill.Tool{WS: ws}
	loadArgs := json.RawMessage(`{"id":"workflows"}`)
	full, err := skill.Run(t.Context(), loadArgs)
	if err != nil {
		t.Fatal(err)
	}
	history := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "load", Name: "load_skill", Arguments: loadArgs}}},
		{Role: llm.RoleTool, ToolCallID: "load", ToolFailed: new(false), Content: full},
	}
	catalog := &Capabilities{Registry: agent.NewRegistry(), Native: []*NativePackage{{
		ID: "native_workflows", Version: "2", Exposure: Lazy, Instructions: workspace.WorkflowGuide + "\nChanged instructions.", Tools: []agent.Tool{&nativeInterfaceProbe{}},
	}}}
	enable := EnableCapability{Catalog: catalog}
	args := json.RawMessage(`{"id":"native_workflows"}`)
	changed, err := enable.Run(t.Context(), args)
	if err != nil {
		t.Fatal(err)
	}
	if enable.ReuseInContext(args, changed, history) != changed {
		t.Fatal("new activation instructions were replaced by an older guide")
	}
}

func TestWorkflowGuideReusePreservesOverridesAndIncompleteActivation(t *testing.T) {
	for _, source := range []string{"workspace", "library", "identical workspace"} {
		t.Run(source, func(t *testing.T) {
			root, library := t.TempDir(), t.TempDir()
			base := filepath.Join(root, "skills", "workflows")
			if source == "library" {
				base = filepath.Join(library, "workflows")
			}
			err := os.MkdirAll(base, 0700)
			if err != nil {
				t.Fatal(err)
			}
			body := "---\nname: workflows\ndescription: Custom guide\n---\nCustom instructions."
			if source == "identical workspace" {
				body = workspace.WorkflowGuide
			}
			err = os.WriteFile(filepath.Join(base, "SKILL.md"), []byte(body), 0600)
			if err != nil {
				t.Fatal(err)
			}
			ws, err := workspace.NewWithSkillRoots(root, []string{library})
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			skill := toolskill.Tool{WS: ws}
			loadArgs := json.RawMessage(`{"id":"workflows"}`)
			custom, err := skill.Run(t.Context(), loadArgs)
			if err != nil {
				t.Fatal(err)
			}
			catalog := &Capabilities{Registry: agent.NewRegistry(), Native: []*NativePackage{{ID: "native_workflows", Version: "1", Exposure: Lazy, Instructions: workspace.WorkflowGuide, Tools: []agent.Tool{&nativeInterfaceProbe{}}}}}
			enable := EnableCapability{Catalog: catalog}
			enableArgs := json.RawMessage(`{"id":"native_workflows"}`)
			activation, err := enable.Run(t.Context(), enableArgs)
			if err != nil {
				t.Fatal(err)
			}
			success := false
			history := []llm.Message{
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "load", Name: "load_skill", Arguments: loadArgs}}},
				{Role: llm.RoleTool, ToolCallID: "load", ToolFailed: &success, Content: custom},
			}
			if enable.ReuseInContext(enableArgs, activation, history) != activation {
				t.Fatal("custom skill suppressed the built-in activation guide")
			}
			history = []llm.Message{
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "enable", Name: "enable_capability", Arguments: enableArgs}}},
				{Role: llm.RoleTool, ToolCallID: "enable", ToolFailed: &success, Harness: "capability_catalog:" + catalog.Save()[0].Fingerprint, Content: activation},
			}
			if skill.ReuseInContext(loadArgs, custom, history) != custom {
				t.Fatal("activation suppressed the custom skill guide")
			}
			for _, incomplete := range []string{activation[:len(activation)-1], "output stored externally", activation + "\ninjected"} {
				if enable.ReuseInContext(enableArgs, incomplete, history) != incomplete {
					t.Fatal("incomplete activation output was replaced")
				}
			}
		})
	}
}
