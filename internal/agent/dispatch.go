package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// InvokeTool is only a model-facing envelope. The Agent resolves it before
// target validation, admission, authorization, scheduling and execution.
type InvokeTool struct{}

func (InvokeTool) Name() string { return "invoke_tool" }
func (InvokeTool) Description() string {
	return "Call an enabled optional tool using its discovered schema."
}
func (InvokeTool) Mode() ToolMode { return Exclusive }
func (InvokeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"arguments":{"type":"object","additionalProperties":true}},"required":["name","arguments"],"additionalProperties":false}`)
}
func (InvokeTool) Run(context.Context, json.RawMessage) (string, error) {
	return "", fmt.Errorf("invoke_tool requires Agent target resolution")
}

// ModelDefs is stable as optional capabilities activate. Defs remains the full
// execution catalog for inspection, validation and saved capability state.
func (r *Registry) ModelDefs() []llm.ToolDef {
	defs := r.Defs()
	if r == nil {
		return defs
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := defs[:0]
	for _, def := range defs {
		if !r.optional[def.Name] {
			out = append(out, def)
		}
	}
	return out
}

func (r *Registry) ResolveCall(call llm.ToolCall) (llm.ToolCall, error) {
	if call.Name != "invoke_tool" || call.InputError != "" {
		return call, nil
	}
	if err := r.ValidateCall(call.Name, call.Arguments); err != nil {
		return call, err
	}
	var envelope struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(call.Arguments, &envelope); err != nil {
		return call, err
	}
	if !r.isOptional(envelope.Name) {
		return call, fmt.Errorf("optional tool %q is not enabled; discover and enable its capability first", envelope.Name)
	}
	call.Name, call.Arguments = envelope.Name, envelope.Arguments
	return call, nil
}

func (r *Registry) isOptional(name string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.optional[name]
}

// ObservedMessage resolves names for UI/eval observers only. The original
// assistant call is retained unchanged in the archive and model replay.
func (r *Registry) ObservedMessage(msg llm.Message) llm.Message {
	msg.ToolCalls = append([]llm.ToolCall(nil), msg.ToolCalls...)
	for i, call := range msg.ToolCalls {
		if target, err := r.ResolveCall(call); err == nil {
			msg.ToolCalls[i] = target
		}
	}
	return msg
}

func CapabilityCatalog(state conversation.Capability) string {
	type definition struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	}
	defs := make([]definition, 0, len(state.Tools))
	for _, tool := range state.Tools {
		defs = append(defs, definition{tool.Name, tool.Description, tool.Parameters})
	}
	data, _ := json.Marshal(struct {
		ID    string       `json:"capability"`
		Tools []definition `json:"tools"`
	}{state.ID, defs})
	return fmt.Sprintf("Capability %s enabled for this session. Schemas below; call invoke_tool for tools absent from the leading tool list.\n%s\n%s", state.ID, data, state.Instructions)
}
