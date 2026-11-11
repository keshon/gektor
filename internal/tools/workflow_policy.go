package tools

import (
	"context"
	"encoding/json"
	"path/filepath"

	imagecontract "github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/permission"
	workflow "github.com/keshon/gektor/internal/workflow/agenttools"
	"github.com/keshon/gektor/internal/workspace"
)

// NewWorkflow wires the same nested permissions for model-free CLI execution.
// Its caller must also authorize the outer workflow call.
func NewWorkflow(d Dependencies, validateOnly bool) *workflow.Tool {
	d = d.prepared()
	return d.workflowTool(validateOnly)
}

func workflowPolicy(ws *workspace.Workspace, policy permission.Policy, gate permission.Gate) (func(context.Context, string, json.RawMessage) error, imagecontract.OutputAuthorizer) {
	output := func(ctx context.Context, path string, args json.RawMessage) error {
		rel, err := ws.Relative(path)
		if err != nil {
			return err
		}
		return permission.Authorize(ctx, policy, gate, "image_variants", filepath.ToSlash(rel), args)
	}
	authorize := func(ctx context.Context, name string, args json.RawMessage) error {
		paths, err := (WorkspacePaths{WS: ws}).Resources(name, args)
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
