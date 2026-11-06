package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation/service"
	"github.com/keshon/gektor/internal/automation/store"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/workspace"
)

type Schema struct {
	WS       *workspace.Workspace
	StoreDir string
}

func (Schema) Name() string { return "workflow_schema" }
func (Schema) Description() string {
	return "Read the workflow format and allowed operation schemas. Optional record_kind reads that kind's registered data schema. Does not execute operations."
}
func (Schema) Mode() agent.ToolMode { return agent.Concurrent }
func (Schema) ReadOnly() bool       { return true }
func (Schema) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"record_kind":{"type":"string","pattern":"^[a-z0-9_-]{1,64}$"}},"additionalProperties":false}`)
}
func (s Schema) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := agent.NewRegistry(s).ValidateCall(s.Name(), args); err != nil {
		return "", err
	}
	var in struct {
		Kind string `json:"record_kind"`
	}
	if err := decode(args, &in); err != nil {
		return "", err
	}
	if in.Kind != "" {
		if s.WS == nil {
			return "", fmt.Errorf("workspace unavailable")
		}
		store, err := store.OpenStore(s.StoreDir, s.WS.Root())
		if err != nil {
			return "", err
		}
		defer store.Close()
		raw, err := store.RecordSchema(in.Kind)
		return string(raw), err
	}
	registry, err := app.AutomationRegistry(s.WS, s.StoreDir, nil)
	if err != nil {
		return "", err
	}
	operations := registry.Schemas()
	data, err := json.Marshal(struct {
		Recipe     json.RawMessage            `json:"recipe"`
		Operations map[string]json.RawMessage `json:"operations"`
		References string                     `json:"references"`
		Examples   []json.RawMessage          `json:"argument_examples"`
	}{definitionTool{}.Schema(), operations, `v3 references are JSON objects, not strings. No interpolation or workspace prefix is needed. A variants item has its written workspace path in output. Validation needs values or defaults for referenced inputs.`, []json.RawMessage{
		json.RawMessage(`{"paths":[{"$input":"source"}]}`),
		json.RawMessage(`{"paths":[{"$input":"source"},{"$step":"write","pointer":"/items/1/output"}]}`),
	}})
	return string(data), err
}

// Save writes only the recipe path selected by the operator. Replacement uses
// rename rather than following a final symlink or modifying a hard-linked file.
type Save struct {
	WS    *workspace.Workspace
	Path  string
	Saved bool
}

func (Save) Name() string { return "workflow_save" }
func (t Save) Description() string {
	return fmt.Sprintf("Save a workflow definition to %q. Accepts a recipe object; no other destination or file operation is available. Validate the saved file separately before claiming it is ready to execute.", t.Path)
}
func (Save) Mode() agent.ToolMode { return agent.Exclusive }
func (Save) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"recipe":{"type":"object"}},"required":["recipe"],"additionalProperties":false}`)
}
func (t *Save) Run(ctx context.Context, args json.RawMessage) (out string, err error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := agent.NewRegistry(t).ValidateCall(t.Name(), args); err != nil {
		return "", err
	}
	var in struct {
		Recipe json.RawMessage `json:"recipe"`
	}
	if err := decode(args, &in); err != nil {
		return "", err
	}

	registry, err := app.AutomationRegistry(t.WS, "", nil)
	if err != nil {
		return "", err
	}
	out, err = (service.Service{WS: t.WS, Registry: registry}).Save(ctx, t.Path, in.Recipe)
	if err == nil {
		t.Saved = true
	}
	return out, err
}
