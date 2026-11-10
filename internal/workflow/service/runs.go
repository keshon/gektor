// Package service exposes saved recipes independently of agent tools.
package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/engine"
	"github.com/keshon/gektor/internal/workflow/recipe"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

type Service struct {
	ScheduleID        string
	ScheduledFor      *time.Time
	OnRun             func(string) error
	WS                *workspace.Workspace
	StoreDir          string
	ValidateOnly      bool
	Registry          *workflow.Registry
	AuthorizeResource func(context.Context, string, string, json.RawMessage) error
	Authorize         func(context.Context, string, json.RawMessage) error
}

func (t Service) ReadOnly() bool { return t.ValidateOnly }

func (t Service) Name() string {
	if t.ValidateOnly {
		return "workflow_validate"
	}
	return "workflow_run"
}
func (t Service) Description() string {
	if t.ValidateOnly {
		return "Validate saved recipe_path with optional typed inputs. Recipes support typed JSON input/output references. Use workflow_schema for format and operation schemas. Record data schemas and output-dependent values are checked during execution."
	}
	return "Read and revalidate recipe_path, then execute that saved recipe once; stop on failed or blocked steps. Returns run record path and evidence. Scheduling is managed by the local service; retry and review behavior comes from the recipe."
}
func (t Service) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"recipe_path":{"type":"string","minLength":1},"inputs":{"type":"object","maxProperties":32}},"required":["recipe_path"],"additionalProperties":false}`)
}

type definitionTool struct{ Service }

func (t definitionTool) Schema() json.RawMessage {
	raw := json.RawMessage(`{"type":"object","properties":{"version":{"type":"integer","const":3},"name":{"type":"string","minLength":1,"maxLength":120},"inputs":{"type":"object","maxProperties":32,"additionalProperties":{"type":"object","properties":{"type":{"type":"string","enum":["string","integer","number","boolean","array","object"]},"required":{"type":"boolean"},"default":{}},"required":["type"],"additionalProperties":false}},"steps":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"object","properties":{"id":{"type":"string","minLength":1,"maxLength":64},"tool":{"type":"string","enum":[]},"review":{"type":"boolean"},"for_each":{},"node_version":{"type":"integer","minimum":1},"retry":{"type":"object","properties":{"max_attempts":{"type":"integer","minimum":1,"maximum":3},"delay_ms":{"type":"integer","minimum":0,"maximum":10000}},"required":["max_attempts"],"additionalProperties":false},"depends_on":{"type":"array","items":{"type":"string"},"uniqueItems":true},"when":{},"merge":{"type":"string","enum":["all","any"]},"args":{"type":"object"}},"required":["id","tool","args"],"additionalProperties":false}}},"required":["version","name","steps"],"additionalProperties":false}`)
	var schema map[string]any
	_ = json.Unmarshal(raw, &schema)
	schema["properties"].(map[string]any)["steps"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["tool"].(map[string]any)["enum"] = t.Registry.Names()
	schema["properties"].(map[string]any)["outputs"] = map[string]any{"type": "object", "maxProperties": 32, "description": "Named values returned by this recipe. Required for recipes used by workflow_call. Values may use $input and $step references."}
	schema["properties"].(map[string]any)["steps"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["id"].(map[string]any)["pattern"] = `^[^@/][^/]*$`
	data, _ := json.Marshal(schema)
	return data
}

// Authorization binds the executable contract and exact arguments, never presentation IDs.
type authorization struct {
	id      string
	tool    string
	version int
	args    string
}

func authorizationFor(s workflow.Step) authorization {
	v := s.NodeVersion
	if v == 0 {
		v = 1
	}
	return authorization{s.ID, s.Tool, v, string(s.Args)}
}

type handler struct {
	artifacts  func(string) workflow.ArtifactAccess
	registry   *workflow.Registry
	authorize  func(context.Context, string, json.RawMessage) error
	authorized map[authorization]bool
	templates  bool
}

func (h handler) Validate(s workflow.Step) error {
	n, err := h.registry.Node(s.Tool)
	if err != nil {
		return err
	}
	version := s.NodeVersion
	if version == 0 {
		version = 1
	}
	if version != n.Version {
		return fmt.Errorf("node %s version %d unavailable (registered %d)", s.Tool, version, n.Version)
	}
	if s.Retry != nil {
		if s.Retry.MaxAttempts < 1 || s.Retry.MaxAttempts > 3 || s.Retry.DelayMS < 0 || s.Retry.DelayMS > 10000 {
			return fmt.Errorf("invalid retry limits")
		}
		if s.Retry.MaxAttempts > 1 && !n.RetrySafe {
			return fmt.Errorf("node %s does not permit retries", s.Tool)
		}
	}

	if len(s.ForEach) > 0 {
		var base map[string]json.RawMessage
		if err := json.Unmarshal(s.Args, &base); err != nil || len(base) != 0 {
			return fmt.Errorf("for_each requires empty args; each item is a complete argument object")
		}
		if h.templates && recipe.HasOutputReferences(s.ForEach) {
			return nil
		}
		var items []json.RawMessage
		if err := json.Unmarshal(s.ForEach, &items); err != nil || string(s.ForEach) == "null" || len(items) > 100 {
			return fmt.Errorf("for_each must be an array of at most 100 argument objects")
		}
		s.ForEach = nil
		for _, item := range items {
			s.Args = item
			if err := h.Validate(s); err != nil {
				return err
			}
		}
		return nil
	}

	if h.templates {
		if n.ValidateTemplate != nil {
			if err := n.ValidateTemplate(s.Args); err != nil {
				return err
			}
		}
		if recipe.HasOutputReferences(s.Args) {
			return workflow.ValidateJSON(s.Args, workflow.TemplateSchema(n.InputSchema))
		}
	}
	if err := workflow.ValidateJSON(s.Args, n.InputSchema); err != nil {
		return err
	}
	if n.Validate != nil {
		return n.Validate(s.Args)
	}
	return nil
}
func (h handler) Run(ctx context.Context, s workflow.Step) (string, error) {
	n, err := h.registry.Node(s.Tool)
	if err != nil {
		return "", err
	}
	if !h.authorized[authorizationFor(s)] {
		if err := h.authorize(ctx, s.Tool, s.Args); err != nil {
			return "", err
		}
	}
	if h.artifacts != nil {
		ctx = workflow.WithArtifacts(ctx, h.artifacts(s.ID))
	}
	out, err := n.Execute(ctx, s.Args)
	if err == nil && len(n.OutputSchema) > 0 {
		err = workflow.ValidateJSON(json.RawMessage(out), n.OutputSchema)
	}
	return out, err
}
func (t Service) authorizeNode(ctx context.Context, name string, args json.RawMessage) error {
	n, err := t.Registry.Node(name)
	if err != nil {
		return err
	}
	if n.Internal && n.PermissionName == "" {
		return nil
	}
	if n.PermissionName != "" {
		name = n.PermissionName
	}
	resources, err := n.Resources(args)
	if err != nil {
		return err
	}
	if t.AuthorizeResource == nil {
		if t.Authorize != nil {
			return t.Authorize(ctx, name, args)
		}
		return fmt.Errorf("node resource authorization unavailable")
	}
	if len(resources) == 0 {
		resources = []string{""}
	}
	for _, resource := range resources {
		if err := t.AuthorizeResource(ctx, name, resource, args); err != nil {
			return err
		}
	}
	return nil
}
func (t Service) DefinitionSchema() json.RawMessage { return (definitionTool{t}).Schema() }
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

func (t Service) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if len(args) > 64<<10 {
		return "", fmt.Errorf("workflow arguments exceed 64 KiB")
	}
	if err := workflow.ValidateJSON(args, t.Schema()); err != nil {
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

	source, err := t.SnapshotRecipe(ctx, in.RecipePath)
	if err != nil {
		return "", err
	}
	return t.RunSource(ctx, source, in.RecipePath, in.Inputs)
}

// Snapshot reads an authorized, bounded workspace recipe for execution or queueing.
func (t Service) Snapshot(ctx context.Context, path string) ([]byte, error) {
	if t.WS == nil || t.Authorize == nil {
		return nil, fmt.Errorf("workflow authorization or workspace unavailable")
	}
	readArgs, _ := json.Marshal(map[string]string{"path": path})
	if err := t.Authorize(ctx, "read_file", readArgs); err != nil {
		return nil, err
	}
	f, err := t.WS.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("recipe must be a regular file")
	}
	source, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(source) > 64<<10 {
		return nil, fmt.Errorf("recipe exceeds 64 KiB")
	}
	return source, nil
}

// RunSource executes the exact saved bytes, including queued snapshots.
func (t Service) RunSource(ctx context.Context, source []byte, path string, inputs map[string]json.RawMessage) (string, error) {
	if t.WS == nil || t.Registry == nil {
		return "", fmt.Errorf("workflow service unavailable")
	}
	if len(source) > 64<<10 {
		return "", fmt.Errorf("recipe exceeds 64 KiB")
	}
	d, source, err := t.Compile(ctx, source, path)
	if err != nil {
		return "", fmt.Errorf("recipe %s: %w; allowed root fields: version, name, steps, inputs, outputs; edit the saved file", path, err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(source))
	h := handler{registry: t.Registry, authorize: t.authorizeNode, authorized: map[authorization]bool{}, templates: true}
	prepared, _, err := recipe.Prepare(d, inputs, h)
	if err != nil {
		return "", err
	}
	if t.ValidateOnly {
		data, _ := json.Marshal(struct {
			Valid      bool   `json:"valid"`
			RecipePath string `json:"recipe_path"`
			SHA256     string `json:"recipe_sha256"`
			Scope      string `json:"scope"`
		}{true, path, digest, "structure, inputs and argument templates; output-dependent values, permissions, record schemas and image compatibility checked during execution"})
		return string(data), nil
	}
	// Authorize known arguments before effects. Output-dependent arguments are
	// authorized in handler.Run after resolution, never with placeholder paths.
	for _, s := range prepared.Steps {
		if s.Review || len(s.ForEach) > 0 || recipe.HasOutputReferences(s.Args) || len(s.When) > 0 || len(s.DependsOn) > 0 {
			continue
		}
		if err := t.authorizeNode(ctx, s.Tool, s.Args); err != nil {
			return "", err
		}
		h.authorized[authorizationFor(s)] = true
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := store.OpenStore(t.StoreDir, t.WS.Root())
	if err != nil {
		return "", err
	}
	defer store.Close()
	w, err := store.NewRun()
	if err != nil {
		return "", err
	}
	defer w.Close()
	if t.OnRun != nil {
		if err := t.OnRun(w.ID()); err != nil {
			return "", err
		}
	}
	save := func(r workflow.Run) error {
		r.ScheduleID, r.ScheduledFor = t.ScheduleID, t.ScheduledFor
		r.RecipePath = path
		r.RecipeSHA256 = digest
		r.RecipeSource = string(source)
		return w.Save(r)
	}
	h.artifacts = w.Artifacts
	r, err := engine.RunWithInputs(ctx, d, inputs, h, save)
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
		Outputs json.RawMessage `json:"outputs,omitempty"`
		ID      string          `json:"run_id"`
		Path    string          `json:"record_path"`
		Status  string          `json:"status"`
		SHA256  string          `json:"recipe_sha256"`
		Steps   []stepSummary   `json:"steps"`
	}{DeclaredOutputs(r), w.ID(), filepath.ToSlash(w.Path()), r.Status, digest, steps})
	if marshalErr != nil {
		return "", marshalErr
	}
	return string(data), err
}

// DeclaredOutputs returns the compiler-collected, completed recipe outputs.
func DeclaredOutputs(r workflow.Run) json.RawMessage {
	for _, s := range r.Steps {
		if s.ID == "@outputs" && s.Status == "completed" {
			return json.RawMessage(s.Output)
		}
	}
	return nil
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
