// Package tools exposes image operations to Agent.
package tools

import (
	"context"
	"os"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/images"
	"github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

type ImageOperation struct{ contract.Operation }
type imageInputs struct{ WS *workspace.Workspace }

func (s imageInputs) IsReference(path string) bool { return llm.IsAttachment(path) }
func (s imageInputs) Open(ctx context.Context, path string) (*os.File, error) {
	return llm.OpenImage(ctx, path, s.WS.Open)
}
func (t ImageOperation) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}
func NewImageOperation(ws *workspace.Workspace, kind string) *ImageOperation {
	return &ImageOperation{contract.Operation{Service: images.Service{WS: ws, Inputs: imageInputs{ws}}, Kind: kind}}
}
