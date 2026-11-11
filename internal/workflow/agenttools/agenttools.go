// Package agenttools adapts the workflow service to agent tool calls.
package agenttools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/keshon/gektor/internal/agent"
	imagecontract "github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workspace"
)

type Tool struct {
	Registry          func() (*workflow.Registry, error)
	WS                *workspace.Workspace
	StoreDir          string
	ValidateOnly      bool
	Authorize         func(context.Context, string, json.RawMessage) error
	AuthorizeResource func(context.Context, string, string, json.RawMessage) error
	AuthorizeOutput   imagecontract.OutputAuthorizer
}

func (t Tool) service() (service.Service, error) {
	registry, err := t.registry()
	authorize := t.AuthorizeResource
	return service.Service{
		WS:                t.WS,
		StoreDir:          t.StoreDir,
		ValidateOnly:      t.ValidateOnly,
		Registry:          registry,
		Authorize:         t.Authorize,
		AuthorizeResource: authorize,
	}, err
}
func (t Tool) Name() string {
	if t.ValidateOnly {
		return "workflow_validate"
	}

	return "workflow_run"
}
func (t Tool) ReadOnly() bool { return t.ValidateOnly }
func (t Tool) Mode() agent.ToolMode {
	if t.ValidateOnly {
		return agent.Concurrent
	}

	return agent.Exclusive
}
func (t Tool) Description() string {
	if t.ValidateOnly {
		return "Validate saved recipe_path with optional typed inputs. Recipes support typed JSON input/output references. Use workflow_schema for format and operation schemas. Record data schemas and output-dependent values are checked during execution."
	}

	return "Read and revalidate recipe_path, then execute that saved recipe once; stop on failed or blocked steps. Returns run record path and evidence. Scheduling is managed by the local service; retry and review behavior comes from the recipe."
}
func (t Tool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"recipe_path":{"type":"string","minLength":1},"inputs":{"type":"object","maxProperties":32}},"required":["recipe_path"],"additionalProperties":false}`)
}

func (t Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	s, err := t.service()
	if err != nil {
		return "", err
	}

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

	return s.Run(ctx, in.RecipePath, in.Inputs)
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
func decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}

	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}

	return nil
}

func (t Tool) registry() (*workflow.Registry, error) {
	if t.Registry == nil {
		return nil, fmt.Errorf("workflow operation registry unavailable")
	}

	return t.Registry()
}
