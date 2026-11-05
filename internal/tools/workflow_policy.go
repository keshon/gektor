package tools

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tools/pixelita"
	"github.com/keshon/gektor/internal/tools/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

// BindWorkflowPolicy gives nested steps the same permission gate as direct calls.
func BindWorkflowPolicy(c *Capabilities, ws *workspace.Workspace, policy permission.Policy, gate func(context.Context, string, string, json.RawMessage) (permission.Effect, error)) {
	output := func(ctx context.Context, path string, args json.RawMessage) error {
		rel, err := ws.Relative(path)
		if err != nil {
			return err
		}
		return permission.Authorize(ctx, policy, gate, "image_variants", filepath.ToSlash(rel), args)
	}
	for _, p := range c.Native {
		for _, tool := range p.Tools {
			if t, ok := tool.(*workflow.Tool); ok {
				t.AuthorizeOutput = output
				t.Authorize = func(ctx context.Context, name string, args json.RawMessage) error {
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
			}
			if t, ok := tool.(*pixelita.Tool); ok {
				t.AuthorizeOutput = output
			}
		}
	}
}
