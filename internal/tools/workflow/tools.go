// Package workflow exposes manual image recipes without arbitrary tool dispatch.
package workflow

import (
	"context"
	"crypto/rand"
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
		return "Validate the saved recipe_path JSON file; does not execute steps or prove image compatibility. Recipe format: {version:1,name,steps:[{id,tool,args}]}; no other fields. Enable native_images for step argument schemas."
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
	return json.RawMessage(`{"type":"object","properties":{"recipe_path":{"type":"string","minLength":1}},"required":["recipe_path"],"additionalProperties":false}`)
}

type definitionTool struct{ Tool }

func (definitionTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"version":{"type":"integer","enum":[1]},"name":{"type":"string","minLength":1,"maxLength":120},"steps":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"object","properties":{"id":{"type":"string","minLength":1,"maxLength":64},"tool":{"type":"string","enum":["image_inspect","image_preview","image_variants","image_compare"]},"args":{"type":"object"}},"required":["id","tool","args"],"additionalProperties":false}}},"required":["version","name","steps"],"additionalProperties":false}`)
}

type handler struct {
	ws              *workspace.Workspace
	authorizeOutput pixelita.OutputAuthorizer
}

func (h handler) tool(s automation.Step) (pixelita.Tool, error) {
	var op string
	switch s.Tool {
	case "image_inspect":
		op = "inspect"
	case "image_preview":
		op = "preview"
	case "image_variants":
		op = "variants"
	case "image_compare":
		op = "compare"
	default:
		return pixelita.Tool{}, fmt.Errorf("unsupported workflow tool %q", s.Tool)
	}
	return pixelita.Tool{WS: h.ws, Operation: op, AuthorizeOutput: h.authorizeOutput}, nil
}
func (h handler) Validate(s automation.Step) error {
	t, err := h.tool(s)
	if err != nil {
		return err
	}
	_, err = t.Validate(s.Args)
	return err
}
func (h handler) Run(ctx context.Context, s automation.Step) (string, error) {
	t, err := h.tool(s)
	if err != nil {
		return "", err
	}
	return t.Run(ctx, s.Args)
}

func Resources(args json.RawMessage) ([]string, error) {
	var in struct {
		RecipePath string `json:"recipe_path"`
	}
	if err := pixelita.Decode(args, &in); err != nil {
		return nil, err
	}
	return []string{in.RecipePath, ".gektor/workflow-runs"}, nil
}

func (t Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if len(args) > 64<<10 {
		return "", fmt.Errorf("workflow arguments exceed 64 KiB")
	}
	if err := agent.NewRegistry(t).ValidateCall(t.Name(), args); err != nil {
		return "", err
	}
	var in struct {
		RecipePath string `json:"recipe_path"`
	}
	if err := pixelita.Decode(args, &in); err != nil {
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
		return "", fmt.Errorf("recipe %s: %w; allowed root fields: version, name, steps; edit the saved file", in.RecipePath, err)
	}
	var d automation.Definition
	if err := pixelita.Decode(source, &d); err != nil {
		return "", fmt.Errorf("recipe %s: %w", in.RecipePath, err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(source))
	h := handler{t.WS, t.AuthorizeOutput}
	if err := automation.Validate(d, h); err != nil {
		return "", err
	}
	if t.ValidateOnly {
		data, _ := json.Marshal(struct {
			Valid      bool   `json:"valid"`
			RecipePath string `json:"recipe_path"`
			SHA256     string `json:"recipe_sha256"`
			Scope      string `json:"scope"`
		}{true, in.RecipePath, digest, "structure and arguments only; image compatibility checked during execution"})
		return string(data), nil
	}
	// Authorize the entire recipe before any step. The outer workflow permission
	// cannot override a deny or approval requirement on an individual image tool.
	for _, s := range d.Steps {
		if err := t.Authorize(ctx, s.Tool, s.Args); err != nil {
			return "", err
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	base := ".gektor/workflow-runs"
	if err := t.WS.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	runDir := filepath.Join(base, rand.Text())
	if err := t.WS.Mkdir(runDir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(runDir, "run.json")
	save := func(r automation.Record) error {
		r.RecipePath = in.RecipePath
		r.RecipeSHA256 = digest
		r.RecipeSource = string(source)
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		tmp := filepath.Join(runDir, "run.tmp")
		if err := t.WS.WriteFile(tmp, data, 0600); err != nil {
			return err
		}
		return t.WS.Rename(tmp, path)
	}
	r, err := automation.Run(ctx, d, h, save)
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
		Path   string        `json:"record_path"`
		Status string        `json:"status"`
		SHA256 string        `json:"recipe_sha256"`
		Steps  []stepSummary `json:"steps"`
	}{filepath.ToSlash(path), r.Status, digest, steps})
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
