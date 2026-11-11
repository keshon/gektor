// Package service loads and freezes recipes, composes nested workflows and
// applies the caller's authorization before execution. CLI, API, agent tools
// and TUI use this layer. It connects the engine to the workspace and store;
// those packages do not construct application clients or operator gates.
package service

import (
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

// Service coordinates workflow execution using borrowed workspace and registry
// dependencies. Construct it by value and keep those dependencies alive during use.
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

// Run loads an authorized recipe and runs or validates it with typed inputs.
func (t Service) Run(ctx context.Context, path string, inputs map[string]json.RawMessage) (string, error) {
	if path == "" {
		return "", fmt.Errorf("recipe_path must not be empty")
	}

	if len(inputs) > 32 {
		return "", fmt.Errorf("workflow inputs exceed 32 properties")
	}

	request, err := json.Marshal(struct {
		RecipePath string                     `json:"recipe_path"`
		Inputs     map[string]json.RawMessage `json:"inputs,omitempty"`
	}{path, inputs})
	if err != nil {
		return "", err
	}

	if len(request) > 64<<10 {
		return "", fmt.Errorf("workflow arguments exceed 64 KiB")
	}

	if t.WS == nil || t.Authorize == nil {
		return "", fmt.Errorf("workflow authorization or workspace unavailable")
	}

	source, err := t.SnapshotRecipe(ctx, path)
	if err != nil {
		return "", err
	}

	return t.RunSource(ctx, source, path, inputs)
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
	h := handler{
		registry:   t.Registry,
		authorize:  t.authorizeNode,
		authorized: map[authorization]bool{},
		templates:  true,
	}
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
		steps = append(steps, stepSummary{
			ID:           s.ID,
			Status:       s.Status,
			Error:        s.Error,
			Sources:      evidence.Sources,
			Items:        evidence.Items,
			FailureStage: evidence.FailureStage,
			Recovery:     evidence.Recovery,
		})
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
