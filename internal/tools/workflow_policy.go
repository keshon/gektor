package tools

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/keshon/gektor/internal/app"
	imagecontract "github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/userdirs"
	workflowdefs "github.com/keshon/gektor/internal/workflow"
	workflow "github.com/keshon/gektor/internal/workflow/agenttools"
	"github.com/keshon/gektor/internal/workspace"
)

// NewWorkflow constructs a standalone workflow tool with nested permissions.
// Its caller must also authorize the outer workflow call.
func NewWorkflow(d Dependencies, validateOnly bool) *workflow.Tool {
	if d.StoreDir == "" {
		d.StoreDir = userdirs.Sub("workflows")
	}
	if len(d.Policy.Rules) == 0 {
		d.Policy = permission.Default()
	}

	authorize, authorizeOutput := workflowPolicy(d.WS, d.Policy, d.Gate)
	authorizeResource := func(ctx context.Context, name, resource string, args json.RawMessage) error {
		return permission.Authorize(ctx, d.Policy, d.Gate, name, resource, args)
	}
	registry := func() (*workflowdefs.Registry, error) {
		return app.WorkflowRegistry(d.WS, d.StoreDir, authorizeOutput, authorizeResource)
	}

	return &workflow.Tool{
		Registry:          registry,
		WS:                d.WS,
		StoreDir:          d.StoreDir,
		ValidateOnly:      validateOnly,
		Authorize:         authorize,
		AuthorizeOutput:   authorizeOutput,
		AuthorizeResource: authorizeResource,
	}
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
		workspacePaths := WorkspacePaths{WS: ws}
		paths, err := workspacePaths.Resources(name, args)
		if err != nil {
			return err
		}
		for _, path := range paths {
			err = permission.Authorize(ctx, policy, gate, name, path, args)
			if err != nil {
				return err
			}
		}
		return nil
	}
	return authorize, output
}
