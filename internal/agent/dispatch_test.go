package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/session"
)

type dispatchedTool struct{ ran *bool }

func (dispatchedTool) Name() string        { return "optional_write" }
func (dispatchedTool) Description() string { return "write fixture" }
func (dispatchedTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
}

type dispatchedRead struct {
	started *atomic.Int32
	ready   chan struct{}
}

func (dispatchedRead) Name() string        { return "optional_read" }
func (dispatchedRead) Description() string { return "read fixture" }
func (dispatchedRead) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false}`)
}
func (dispatchedRead) Mode() ToolMode { return Concurrent }
func (dispatchedRead) ReadOnly() bool { return true }
func (p dispatchedRead) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	if p.started.Add(1) == 2 {
		close(p.ready)
	}
	select {
	case <-p.ready:
		return "read", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestDispatchedReadsRemainConcurrentAndAvailableInPlan(t *testing.T) {
	var started atomic.Int32
	reg := NewRegistry(InvokeTool{})
	if err := reg.AddOptional(dispatchedRead{&started, make(chan struct{})}); err != nil {
		t.Fatal(err)
	}
	calls := []llm.ToolCall{
		{ID: "one", Name: "invoke_tool", Arguments: json.RawMessage(`{"name":"optional_read","arguments":{}}`)},
		{ID: "two", Name: "invoke_tool", Arguments: json.RawMessage(`{"name":"optional_read","arguments":{}}`)},
	}
	client := &stubClient{responses: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: calls}}, say("done")}}
	a := New(Config{Client: client, Tools: reg, ReadOnly: []string{}, AllowReadOnlyExtensions: true})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if _, err := a.Run(ctx, "read"); err != nil {
		t.Fatal(err)
	}
	if started.Load() != 2 || a.Report().ToolsExecuted != 2 {
		t.Fatal(a.Report())
	}
	for _, result := range a.Report().RecentTools {
		if result.Status != "success" {
			t.Fatal(result)
		}
	}
}

func TestRequestIdentitySurvivesCompactionAndExcludesToolImages(t *testing.T) {
	a := New(Config{Tools: NewRegistry(), Directive: "Plan mode"})
	a.snapshot = session.New([]llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "initial task"},
		{Role: llm.RoleAssistant, Content: "Download something"},
		{Role: llm.RoleUser, Harness: "tool_images", Content: "tool image"},
		{Role: llm.RoleTool, Content: "old tool result"},
		{Role: llm.RoleUser, Content: pictureNote([]string{"old.png"}), Images: []string{"old.png"}},
		{Role: llm.RoleUser, Content: "Review only; no downloads"},
		{Role: llm.RoleUser, Harness: "notice", Content: "Act mode notice"},
	})
	req := llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "initial task"}}}
	a.requestIdentity(&req)
	if req.UserRequest != "Review only; no downloads" || req.UserRequestCount != 2 || req.RunDirective != "Plan mode" {
		t.Fatal(req)
	}
}

func TestDispatchedPictureRetainsAttachmentGrantAndUserIdentity(t *testing.T) {
	probe := &attachmentProbe{}
	reg := NewRegistry(InvokeTool{}, probe)
	if err := reg.AddOptional(pictureTool{paths: []string{"tool.png"}}); err != nil {
		t.Fatal(err)
	}
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "picture", Name: "invoke_tool", Arguments: json.RawMessage(`{"name":"read_picture","arguments":{}}`)}}}},
		toolCallStep("attachment_probe"), say("done"),
	}}
	a := New(Config{Client: client, Tools: reg})
	if _, err := a.Run(t.Context(), "inspect picture"); err != nil {
		t.Fatal(err)
	}
	if len(probe.got) != 1 || probe.got[0].Name != "tool.png" {
		t.Fatal(probe.got)
	}
	var req llm.ChatRequest
	a.requestIdentity(&req)
	if req.UserRequest != "inspect picture" || req.UserRequestCount != 1 {
		t.Fatal(req)
	}
}
func (dispatchedTool) Mode() ToolMode { return Exclusive }
func (dispatchedTool) Resources(json.RawMessage) ([]string, error) {
	return []string{"canonical/file"}, nil
}
func (p dispatchedTool) Run(context.Context, json.RawMessage) (string, error) {
	*p.ran = true
	return "done", nil
}

func TestDispatcherPreservesPrefixAndEnforcesTargetChecks(t *testing.T) {
	for _, kind := range []string{"allowed", "denied", "plan", "malformed", "inactive", "recursive"} {
		t.Run(kind, func(t *testing.T) {
			ran := false
			reg := NewRegistry(InvokeTool{})
			before := reg.ModelDefs()
			if err := reg.AddOptional(dispatchedTool{&ran}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, reg.ModelDefs()) {
				t.Fatal("activation changed leading definitions")
			}
			args := json.RawMessage(`{"name":"optional_write","arguments":{"path":"file"}}`)
			switch kind {
			case "malformed":
				args = json.RawMessage(`{"name":"optional_write","arguments":{"unexpected":true}}`)
			case "inactive":
				args = json.RawMessage(`{"name":"missing","arguments":{}}`)
			case "recursive":
				args = json.RawMessage(`{"name":"invoke_tool","arguments":{}}`)
			}
			original := llm.ToolCall{ID: "call", Name: "invoke_tool", Arguments: args}
			client := &stubClient{responses: []llm.ChatResponse{
				{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{original}}},
				{Message: llm.Message{Role: llm.RoleAssistant, Content: "finished"}},
			}}
			checked := false
			cfg := Config{Client: client, Tools: reg, MutatingTools: []string{"optional_write"}, BeforeToolCall: func(_ context.Context, name, resource string, raw json.RawMessage) (permission.Effect, error) {
				checked = true
				if name != "optional_write" || resource != "canonical/file" || string(raw) != `{"path":"file"}` {
					t.Fatal("authorized wrapper instead of target", name, resource, string(raw))
				}
				return permission.Allow, nil
			}}
			if kind == "denied" {
				cfg.Policy = permission.Policy{Rules: []permission.Rule{{Tool: "optional_write", Pattern: "*", Effect: permission.Deny}}}
			}
			if kind == "plan" {
				cfg.ReadOnly = []string{"invoke_tool"}
			}
			if kind == "allowed" {
				cfg.Policy = permission.Policy{Rules: []permission.Rule{{Tool: "optional_write", Pattern: "*", Effect: permission.Ask}}}
			}
			a := New(cfg)
			if _, err := a.Run(t.Context(), "task"); err != nil {
				t.Fatal(err)
			}
			if ran != (kind == "allowed") {
				t.Fatal("wrong execution", ran, kind)
			}
			if kind == "allowed" && (!checked || a.LastRunMutations != 1 || a.Report().RecentTools[0].Name != "optional_write") {
				t.Fatal("lost target effects", a.Report())
			}
			if !reflect.DeepEqual(client.lastHistory[2].ToolCalls[0], original) {
				t.Fatal("rewrote archived invocation", client.lastHistory)
			}
		})
	}
}

func TestCatalogRestorationUsesTrustedMetadata(t *testing.T) {
	ran := false
	reg := NewRegistry(InvokeTool{})
	if err := reg.AddOptional(dispatchedTool{&ran}); err != nil {
		t.Fatal(err)
	}
	state := session.Capability{ID: "fixture", Fingerprint: "fingerprint", Tools: NewRegistry(dispatchedTool{&ran}).Defs()}
	a := New(Config{Tools: reg, SaveCapabilities: func() []session.Capability { return []session.Capability{state} }})
	history := []llm.Message{{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: CapabilityCatalog(state)}}
	restored := a.withCapabilityInstructions(history)
	if len(restored) != 3 || restored[2].Harness != "capability_catalog:fingerprint" || !strings.Contains(restored[2].Content, `"parameters"`) {
		t.Fatal(restored)
	}
	if again := a.withCapabilityInstructions(restored); len(again) != 3 {
		t.Fatal("duplicate catalog", again)
	}
	// A compacted projection has lost the catalog but saved state has not.
	if compacted := a.withCapabilityInstructions(restored[:2]); len(compacted) != 3 {
		t.Fatal("catalog lost after compaction")
	}
}
