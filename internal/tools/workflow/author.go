package workflow

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/tools/pixelita"
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
		store, err := automation.OpenStore(s.StoreDir, s.WS.Root())
		if err != nil {
			return "", err
		}
		defer store.Close()
		raw, err := store.RecordSchema(in.Kind)
		return string(raw), err
	}
	operations := map[string]json.RawMessage{}
	for _, op := range []string{"inspect", "preview", "variants", "compare"} {
		t := pixelita.Tool{Operation: op}
		operations[t.Name()] = t.Schema()
	}
	for _, write := range []bool{false, true} {
		t := Data{Write: write}
		operations[t.Name()] = t.Schema()
	}
	data, err := json.Marshal(struct {
		Recipe     json.RawMessage            `json:"recipe"`
		Operations map[string]json.RawMessage `json:"operations"`
		References string                     `json:"references"`
		Examples   []json.RawMessage          `json:"argument_examples"`
	}{definitionTool{}.Schema(), operations, `v2 references are JSON objects, not strings. No interpolation or workspace prefix is needed. A variants item has its written workspace path in output. Validation needs values or defaults for referenced inputs.`, []json.RawMessage{
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
	if err := agent.NewRegistry(definitionTool{}).ValidateCall("workflow_run", in.Recipe); err != nil {
		return "", err
	}
	// Preserve JSON numbers rather than converting integer inputs through float64.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(in.Recipe, &raw); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if len(data) > 64<<10 {
		return "", fmt.Errorf("recipe exceeds 64 KiB")
	}
	path, err := t.WS.Relative(t.Path)
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(path)
	if err := t.WS.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	tmp := filepath.Join(parent, ".gektor-recipe-"+rand.Text()+".tmp")
	f, err := t.WS.CreateExclusive(tmp, 0600)
	if err != nil {
		return "", err
	}
	defer func() {
		if e := t.WS.Remove(tmp); e != nil && !os.IsNotExist(e) {
			err = errors.Join(err, e)
		}
	}()
	_, writeErr := f.Write(data)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := t.WS.Rename(tmp, path); err != nil {
		return "", err
	}
	t.Saved = true
	data, err = json.Marshal(struct {
		Path  string `json:"recipe_path"`
		Saved bool   `json:"saved"`
	}{filepath.ToSlash(path), true})
	return string(data), err
}
