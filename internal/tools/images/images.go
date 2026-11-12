// Package images owns Agent image grants, viewing and image-operation adapters.
package images

import (
	"context"
	"os"

	"github.com/keshon/gektor/internal/agent"
	imagecap "github.com/keshon/gektor/internal/capabilities/images"
	"github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

type Operation struct{ contract.Operation }
type imageInputs struct{ WS *workspace.Workspace }

func (s imageInputs) IsReference(path string) bool { return llm.IsAttachment(path) }
func (s imageInputs) Open(ctx context.Context, path string) (*os.File, error) {
	return llm.OpenImage(ctx, path, s.WS.Open)
}
func (t Operation) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}
func NewOperation(ws *workspace.Workspace, kind string) *Operation {
	return &Operation{contract.Operation{Service: imagecap.Service{WS: ws, Inputs: imageInputs{ws}}, Kind: kind}}
}
