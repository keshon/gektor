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

type Tool struct{ contract.Operation }
type SessionInputs struct{ WS *workspace.Workspace }

func (s SessionInputs) IsReference(path string) bool { return llm.IsAttachment(path) }
func (s SessionInputs) Open(ctx context.Context, path string) (*os.File, error) {
	return llm.OpenImage(ctx, path, s.WS.Open)
}
func (t Tool) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}
func NewTool(ws *workspace.Workspace, kind string) *Tool {
	return &Tool{contract.Operation{Service: images.Service{WS: ws, Inputs: SessionInputs{ws}}, Kind: kind}}
}
