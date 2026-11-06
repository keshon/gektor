// Package workflow exposes saved recipes without arbitrary tool dispatch.
package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/tools/pixelita"
	"github.com/keshon/gektor/internal/workspace"
)

type Tool struct {
	WS              *workspace.Workspace
	StoreDir        string
	ValidateOnly    bool
	Authorize       func(context.Context, string, json.RawMessage) error
	AuthorizeOutput pixelita.OutputAuthorizer
}

func (t Tool) ReadOnly() bool { return t.ValidateOnly }

func (t Tool) Name() string {
	if t.ValidateOnly {
		return "workflow_validate"
	}
	return "workflow_run"
}
func (t Tool) Description() string {
	if t.ValidateOnly {
		return "Validate saved recipe_path with optional typed inputs. v2 supports JSON input/output references. Use workflow_schema for format and operation schemas. Record data schemas and output-dependent values are checked during execution."
	}
	return "Read and revalidate recipe_path, then execute that saved recipe once; stop on failed or blocked steps. Returns run record path and evidence. No scheduling or retries."
}
func (t Tool) Mode() agent.ToolMode {
	if t.ValidateOnly {
		return agent.Concurrent
	}
	return agent.Exclusive
}
func (t Tool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"recipe_path":{"type":"string","minLength":1},"inputs":{"type":"object","maxProperties":32}},"required":["recipe_path"],"additionalProperties":false}`)
}

type definitionTool struct{ Tool }

func (definitionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"version":{"type":"integer","enum":[1,2]},"name":{"type":"string","minLength":1,"maxLength":120},"inputs":{"type":"object","maxProperties":32,"additionalProperties":{"type":"object","properties":{"type":{"type":"string","enum":["string","integer","number","boolean","array","object"]},"required":{"type":"boolean"},"default":{}},"required":["type"],"additionalProperties":false}},"steps":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"object","properties":{"id":{"type":"string","minLength":1,"maxLength":64},"tool":{"type":"string","enum":["image_inspect","image_preview","image_variants","image_compare","record_read","record_write"]},"args":{"type":"object"}},"required":["id","tool","args"],"additionalProperties":false}}},"required":["version","name","steps"],"additionalProperties":false}`)
}

type handler struct {
	ws              *workspace.Workspace
	storeDir        string
	authorizeOutput pixelita.OutputAuthorizer
	authorize       func(context.Context, string, json.RawMessage) error
	authorized      map[string]bool
	templates       bool
}

func (h handler) tool(s automation.Step) (agent.Tool, error) {
	var op string
	switch s.Tool {
	case "record_read", "record_write":
		return Data{WS: h.ws, StoreDir: h.storeDir, Write: s.Tool == "record_write"}, nil
	case "image_inspect":
		op = "inspect"
	case "image_preview":
		op = "preview"
	case "image_variants":
		op = "variants"
	case "image_compare":
		op = "compare"
	default:
		return nil, fmt.Errorf("unsupported workflow tool %q", s.Tool)
	}
	return pixelita.Tool{WS: h.ws, Operation: op, AuthorizeOutput: h.authorizeOutput}, nil
}
func (h handler) Validate(s automation.Step) error {
	t, err := h.tool(s)
	if err != nil {
		return err
	}
	if h.templates {
		if err := h.validateLiteralPaths(s.Args); err != nil {
			return err
		}
		if automation.HasOutputReferences(s.Args) {
			return h.validateTemplate(t, s.Args)
		}
	}
	if image, ok := t.(pixelita.Tool); ok {
		_, err = image.Validate(s.Args)
		return err
	}
	return agent.NewRegistry(t).ValidateCall(t.Name(), s.Args)
}
func (h handler) Run(ctx context.Context, s automation.Step) (string, error) {
	t, err := h.tool(s)
	if err != nil {
		return "", err
	}
	if !h.authorized[s.ID] {
		if err := h.authorize(ctx, s.Tool, s.Args); err != nil {
			return "", err
		}
	}
	return t.Run(ctx, s.Args)
}

func Resources(args json.RawMessage) ([]string, error) {
	var in struct {
		RecipePath string                     `json:"recipe_path"`
		Inputs     map[string]json.RawMessage `json:"inputs"`
	}
	if err := decode(args, &in); err != nil {
		return nil, err
	}
	return []string{in.RecipePath}, nil
}

func (t Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if len(args) > 64<<10 {
		return "", fmt.Errorf("workflow arguments exceed 64 KiB")
	}
	if err := agent.NewRegistry(t).ValidateCall(t.Name(), args); err != nil {
		return "", err
	}
	var in struct {
		RecipePath string                     `json:"recipe_path"`
		Inputs     map[string]json.RawMessage `json:"inputs"`
	}
	if err := decode(args, &in); err != nil {
		return "", err
	}
	if t.WS == nil || t.Authorize == nil {
		return "", fmt.Errorf("workflow authorization or workspace unavailable")
	}
	readArgs, _ := json.Marshal(struct {
		Path string `json:"path"`
	}{in.RecipePath})
	if err := t.Authorize(ctx, "read_file", readArgs); err != nil {
		return "", err
	}
	f, err := t.WS.Open(in.RecipePath)
	if err != nil {
		return "", err
	}
	source, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	f.Close()
	if err != nil {
		return "", err
	}
	if len(source) > 64<<10 {
		return "", fmt.Errorf("recipe exceeds 64 KiB")
	}
	if err := agent.NewRegistry(definitionTool{t}).ValidateCall(t.Name(), source); err != nil {
		return "", fmt.Errorf("recipe %s: %w; allowed root fields: version, name, steps, inputs (v2); edit the saved file", in.RecipePath, err)
	}
	var d automation.Definition
	if err := decode(source, &d); err != nil {
		return "", fmt.Errorf("recipe %s: %w", in.RecipePath, err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(source))
	h := handler{ws: t.WS, storeDir: t.StoreDir, authorizeOutput: t.AuthorizeOutput, authorize: t.Authorize, authorized: map[string]bool{}, templates: d.Version == 2}
	prepared, _, err := automation.Prepare(d, in.Inputs, h)
	if err != nil {
		return "", err
	}
	if t.ValidateOnly {
		data, _ := json.Marshal(struct {
			Valid      bool   `json:"valid"`
			RecipePath string `json:"recipe_path"`
			SHA256     string `json:"recipe_sha256"`
			Scope      string `json:"scope"`
		}{true, in.RecipePath, digest, "structure, inputs and argument templates; output-dependent values, permissions, record schemas and image compatibility checked during execution"})
		return string(data), nil
	}
	// Authorize known arguments before effects. Output-dependent arguments are
	// authorized in handler.Run after resolution, never with placeholder paths.
	for _, s := range prepared.Steps {
		if d.Version == 2 && automation.HasOutputReferences(s.Args) {
			continue
		}
		if err := t.Authorize(ctx, s.Tool, s.Args); err != nil {
			return "", err
		}
		h.authorized[s.ID] = true
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := automation.OpenStore(t.StoreDir, t.WS.Root())
	if err != nil {
		return "", err
	}
	defer store.Close()
	w, err := store.NewRun()
	if err != nil {
		return "", err
	}
	defer w.Close()
	save := func(r automation.Record) error {
		r.RecipePath = in.RecipePath
		r.RecipeSHA256 = digest
		r.RecipeSource = string(source)
		return w.Save(r)
	}
	r, err := automation.RunWithInputs(ctx, d, in.Inputs, h, save)
	// The full definition and outputs stay in the record, rather than being
	// repeated in model context after every run.
	steps := make([]stepSummary, 0, len(r.Steps))
	for _, s := range r.Steps {
		var evidence struct {
			Sources      json.RawMessage `json:"sources"`
			Items        json.RawMessage `json:"items"`
			FailureStage string          `json:"failure_stage"`
			Recovery     string          `json:"recovery"`
		}
		_ = json.Unmarshal([]byte(s.Output), &evidence)
		steps = append(steps, stepSummary{ID: s.ID, Status: s.Status, Error: s.Error, Sources: evidence.Sources, Items: evidence.Items, FailureStage: evidence.FailureStage, Recovery: evidence.Recovery})
	}
	data, marshalErr := json.Marshal(struct {
		ID     string        `json:"run_id"`
		Path   string        `json:"record_path"`
		Status string        `json:"status"`
		SHA256 string        `json:"recipe_sha256"`
		Steps  []stepSummary `json:"steps"`
	}{w.ID(), filepath.ToSlash(w.Path()), r.Status, digest, steps})
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(data), err
}

type stepSummary struct {
	FailureStage string          `json:"failure_stage,omitempty"`
	Recovery     string          `json:"recovery,omitempty"`
	ID           string          `json:"id"`
	Status       string          `json:"status"`
	Error        string          `json:"error,omitempty"`
	Sources      json.RawMessage `json:"sources,omitempty"`
	Items        json.RawMessage `json:"items,omitempty"`
}

// decode accepts one JSON value, rejecting accidental concatenated arguments.
func decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}
	return nil
}
