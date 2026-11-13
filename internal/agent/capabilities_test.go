package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
)

type capabilityWrite struct{ ran *bool }

func (capabilityWrite) Name() string        { return "optional_write" }
func (capabilityWrite) Description() string { return "write fixture" }
func (capabilityWrite) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
}

type capabilityRead struct {
	started *atomic.Int32
	ready   chan struct{}
}

func (capabilityRead) Name() string        { return "optional_read" }
func (capabilityRead) Description() string { return "read fixture" }
func (capabilityRead) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false}`)
}
func (capabilityRead) Mode() ToolMode { return Concurrent }
func (capabilityRead) ReadOnly() bool { return true }
func (p capabilityRead) Run(ctx context.Context, _ json.RawMessage) (string, error) {
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

func TestEnabledReadsRemainConcurrentAndAvailableInPlan(t *testing.T) {
	var started atomic.Int32
	reg := NewRegistry()
	if err := reg.Add(capabilityRead{&started, make(chan struct{})}); err != nil {
		t.Fatal(err)
	}
	calls := []llm.ToolCall{
		{ID: "one", Name: "optional_read", Arguments: json.RawMessage(`{}`)},
		{ID: "two", Name: "optional_read", Arguments: json.RawMessage(`{}`)},
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
	a.snapshot = conversation.New([]llm.Message{
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

func TestEnabledPictureRetainsAttachmentGrantAndUserIdentity(t *testing.T) {
	probe := &attachmentProbe{}
	reg := NewRegistry(probe)
	if err := reg.Add(pictureTool{paths: []string{"tool.png"}}); err != nil {
		t.Fatal(err)
	}
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "picture", Name: "read_picture", Arguments: json.RawMessage(`{}`)}}}},
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
func (capabilityWrite) Mode() ToolMode { return Exclusive }
func (capabilityWrite) Resources(json.RawMessage) ([]string, error) {
	return []string{"canonical/file"}, nil
}
func (p capabilityWrite) Run(context.Context, json.RawMessage) (string, error) {
	*p.ran = true
	return "done", nil
}

func TestCatalogRestorationUsesTrustedMetadata(t *testing.T) {
	ran := false
	reg := NewRegistry()
	if err := reg.Add(capabilityWrite{&ran}); err != nil {
		t.Fatal(err)
	}
	state := conversation.Capability{ID: "fixture", Fingerprint: "fingerprint", Tools: NewRegistry(capabilityWrite{&ran}).Defs(), Instructions: "fixture guidance"}
	a := New(Config{Tools: reg, Catalog: &savedCatalog{states: []conversation.Capability{state}}})
	history := []llm.Message{{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: CapabilityCatalog(state)}}
	restored := a.withCapabilityInstructions(history)
	if len(restored) != 3 || restored[2].Harness != "capability_catalog:fingerprint" || !strings.Contains(restored[2].Content, "optional_write") || strings.Contains(restored[2].Content, `"parameters"`) {
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

// savedCatalog supplies persisted activation state for result attachment tests.
type savedCatalog struct{ states []conversation.Capability }

func (*savedCatalog) Prepare(context.Context) error                            { return nil }
func (*savedCatalog) PreviewDefs() []llm.ToolDef                               { return nil }
func (c *savedCatalog) Save() []conversation.Capability                        { return c.states }
func (*savedCatalog) Restore(context.Context, []conversation.Capability) error { return nil }

func TestDirectOptionalSchemaAndAdmission(t *testing.T) {
	for _, kind := range []string{"allowed", "denied", "plan", "invalid", "inactive"} {
		t.Run(kind, func(t *testing.T) {
			ran := false
			registry := NewRegistry()
			tool := capabilityWrite{&ran}
			if kind != "inactive" {
				err := registry.Add(tool)
				if err != nil {
					t.Fatal(err)
				}
			}
			call := llm.ToolCall{ID: "direct", Name: tool.Name(), Arguments: json.RawMessage(`{"path":"file"}`)}
			if kind == "invalid" {
				call.Arguments = json.RawMessage(`{}`)
			}
			client := &repeatSeqClient{responses: []llm.ChatResponse{stepOf(call), say("finished")}}
			checked := false
			cfg := Config{
				Client: client, Tools: registry, MutatingTools: []string{"optional_write"},
				Policy: permission.Policy{Rules: []permission.Rule{{Tool: "*", Pattern: "*", Effect: permission.Ask}}},
				BeforeToolCall: func(ctx context.Context, name, resource string, args json.RawMessage) (permission.Effect, error) {
					checked = true
					if name != "optional_write" || resource != "canonical/file" {
						t.Fatal("non-canonical authorization", name, resource)
					}
					return permission.Allow, nil
				},
			}
			if kind == "denied" {
				cfg.Policy.Rules[0].Effect = permission.Deny
			}
			if kind == "plan" {
				cfg.ReadOnly = []string{"read_file"}
			}
			a := New(cfg)
			_, err := a.Run(t.Context(), "task")
			if err != nil {
				t.Fatal(err)
			}
			if ran != (kind == "allowed") || checked != (kind == "allowed") {
				t.Fatal("direct call bypassed admission", kind, ran, checked)
			}
			if kind == "allowed" && a.LastRunMutations != 1 {
				t.Fatal("lost direct mutation tracking")
			}
			if !reflect.DeepEqual(client.reqs[1].Messages[2].ToolCalls[0], call) {
				t.Fatal("rewrote archived direct call")
			}
			if kind != "inactive" {
				defs := client.reqs[0].Tools
				schema := tool.Schema()
				if len(defs) != 1 || defs[0].Name != "optional_write" || !reflect.DeepEqual(defs[0].Parameters, schema) {
					t.Fatal("actual request lacks target schema", defs)
				}
			}
		})
	}
}

func TestUnloadedInstructionsAreRemovedFromProjectionOnly(t *testing.T) {
	state := conversation.Capability{ID: "fixture", Fingerprint: "catalog", Instructions: "Private operational instructions", Unloaded: true}
	a := New(Config{Tools: NewRegistry(), Catalog: &savedCatalog{states: []conversation.Capability{state}}})
	history := []llm.Message{{Role: llm.RoleTool, Harness: "capability_catalog:catalog", Content: "Private operational instructions"}}
	projected := a.withCapabilityInstructions(history)
	if projected[0].Content == history[0].Content || history[0].Content != "Private operational instructions" {
		t.Fatal("unload retained active instructions or mutated the archive")
	}
}
