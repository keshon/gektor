package tools

import (
	"context"
	"encoding/json"
	"path/filepath"

	workflow "github.com/keshon/gektor/internal/automation/agenttools"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/integrations/pixelita"
	imageagent "github.com/keshon/gektor/internal/integrations/pixelita/agenttools"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workspace"
)

// BindWorkflowPolicy gives nested steps the same permission gate as direct calls.
func BindWorkflowPolicy(c *Capabilities, ws *workspace.Workspace, policy permission.Policy, gate func(context.Context, string, string, json.RawMessage) (permission.Effect, error), storeDir string) {
	if storeDir == "" {
		storeDir = gektor.Sub("workflows")
	}
	authorize, output := workflowPolicy(ws, policy, gate)
	for _, p := range c.Native {
		for _, tool := range p.Tools {
			if t, ok := tool.(*workflow.Schema); ok {
				t.StoreDir = storeDir
			}
			if t, ok := tool.(*workflow.Data); ok {
				t.StoreDir = storeDir
			}
			if t, ok := tool.(*workflow.Inspect); ok {
				t.StoreDir = storeDir
			}
			if t, ok := tool.(*workflow.Tool); ok {
				t.AuthorizeOutput, t.Authorize = output, authorize
				t.AuthorizeResource = func(ctx context.Context, name, resource string, args json.RawMessage) error {
					return permission.Authorize(ctx, policy, gate, name, resource, args)
				}
				t.StoreDir = storeDir
			}
			if t, ok := tool.(*imageagent.Tool); ok {
				t.AuthorizeOutput = output
			}
		}
	}
}

// NewWorkflow wires the same nested permissions for model-free CLI execution.
// Its caller must also authorize the outer workflow call.
func NewWorkflow(ws *workspace.Workspace, validateOnly bool, policy permission.Policy, gate permission.Gate) *workflow.Tool {
	authorize, output := workflowPolicy(ws, policy, gate)
	return &workflow.Tool{WS: ws, StoreDir: gektor.Sub("workflows"), ValidateOnly: validateOnly, Authorize: authorize, AuthorizeOutput: output, AuthorizeResource: func(ctx context.Context, name, resource string, args json.RawMessage) error {
		return permission.Authorize(ctx, policy, gate, name, resource, args)
	}}
}

func workflowPolicy(ws *workspace.Workspace, policy permission.Policy, gate permission.Gate) (func(context.Context, string, json.RawMessage) error, pixelita.OutputAuthorizer) {
	output := func(ctx context.Context, path string, args json.RawMessage) error {
		rel, err := ws.Relative(path)
		if err != nil {
			return err
		}
		return permission.Authorize(ctx, policy, gate, "image_variants", filepath.ToSlash(rel), args)
	}
	authorize := func(ctx context.Context, name string, args json.RawMessage) error {
		paths, err := PathResources(ws, name, args)
		if err != nil {
			return err
		}
		for _, path := range paths {
			if err := permission.Authorize(ctx, policy, gate, name, path, args); err != nil {
				return err
			}
		}
		return nil
	}
	return authorize, output
}
