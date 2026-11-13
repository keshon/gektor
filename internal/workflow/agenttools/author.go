package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

type Schema struct {
	Registry func() (*workflow.Registry, error)
	WS       *workspace.Workspace
	StoreDir string
}

func (Schema) Name() string { return "workflow_schema" }
func (Schema) Description() string {
	return "Read the current recipe format, operation schemas and native schedule-trigger instructions. Optional record_kind reads that kind's registered data schema. Does not execute operations."
}
func (Schema) Mode() agent.ToolMode { return agent.Concurrent }
func (Schema) ReadOnly() bool       { return true }
func (Schema) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"record_kind":{"type":"string","pattern":"^[a-z0-9_-]{1,64}$"}},"additionalProperties":false}`)
}
func (s Schema) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if s.Registry == nil {
		return "", fmt.Errorf("workflow operation registry unavailable")
	}
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
	registry, err := s.Registry()
	if err != nil {
		return "", err
	}
	operations := registry.Schemas()
	data, err := json.Marshal(struct {
		Recipe     json.RawMessage              `json:"recipe"`
		Operations map[string]json.RawMessage   `json:"operations"`
		Contracts  map[string]workflow.Contract `json:"operation_contracts"`
		References string                       `json:"references"`
		Examples   []json.RawMessage            `json:"argument_examples"`
		Scheduling json.RawMessage              `json:"schedule_trigger"`
	}{(service.Service{Registry: registry}).DefinitionSchema(), operations, registry.Contracts(), `Use operation_contracts for descriptions, inputs, outputs and retry safety. output_schema=true means the return value depends on the operation arguments or script. script_transform runs bundled JavaScript and returns exactly its JSON value, without an /output wrapper. Recipe outputs are VALUES or references, not type declarations; steps have no outputs field. v4 references are JSON objects, not strings. No interpolation or workspace prefix is needed. Validation needs values or defaults for referenced inputs. workflow_call returns the called recipe's declared outputs object.`, []json.RawMessage{
		json.RawMessage(`{"version":4,"name":"Double count","inputs":{"count":{"type":"integer","default":3}},"steps":[{"id":"double","tool":"script_transform","args":{"input":{"$input":"count"},"code":"return input * 2;"}}],"outputs":{"doubled":{"$step":"double"}}}`),
		json.RawMessage(`{"paths":[{"$input":"source"}]}`),
		json.RawMessage(`{"paths":[{"$input":"source"},{"$step":"write","pointer":"/items/1/output"}]}`),
	}, json.RawMessage(`{"kind":"schedule","tool":"workflow_schedule","list_tool":"workflow_schedules","status_tool":"workflow_status","storage":"workspace user store, outside recipe JSON","description":"Native cron triggers start saved recipes. Do not add a schedule operation to steps. They appear as Schedule cards in the TUI. No Windows Task Scheduler or system cron is needed.","service_required":true,"service_command":"gektor workflow serve -workspace <workspace>","snapshot":"Creating a schedule freezes the saved recipe and input values; later recipe edits do not update it. Update requires expected_revision from workflow_schedules; refresh_recipe=true explicitly replaces the frozen recipe. workflow_status shows worker ownership and job/run IDs.","example":{"action":"create","id":"uptime-minute","recipe_path":"uptime-checker-recipe.json","cron":"* * * * *","timezone":"UTC"}}`)})
	return string(data), err
}

// Save writes only the recipe path selected by the operator. Replacement uses
// rename rather than following a final symlink or modifying a hard-linked file.
type Save struct {
	Registry func() (*workflow.Registry, error)
	WS       *workspace.Workspace
	Path     string
	Saved    bool
}

// Resources identifies the operator-selected destination for permission checks.
func (t Save) Resources(json.RawMessage) ([]string, error) {
	path, err := t.WS.Relative(t.Path)
	if err != nil {
		return nil, err
	}
	return []string{filepath.ToSlash(path)}, nil
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
	if t.Registry == nil {
		return "", fmt.Errorf("workflow operation registry unavailable")
	}
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

	registry, err := t.Registry()
	if err != nil {
		return "", err
	}
	out, err = (service.Service{WS: t.WS, Registry: registry}).Save(ctx, t.Path, in.Recipe)
	if err == nil {
		t.Saved = true
	}
	return out, err
}
