// Package nodes exposes image operations as workflow nodes.
package nodes

import (
	"github.com/keshon/gektor/internal/capabilities/images"
	"github.com/keshon/gektor/internal/capabilities/images/contract"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

func ImageDefinitions(ws *workspace.Workspace, output contract.OutputAuthorizer) []workflow.Node {
	var nodes []workflow.Node
	for _, name := range contract.Names() {
		op := contract.Operation{Service: images.Service{WS: ws}, Kind: name, AuthorizeOutput: output}
		nodes = append(nodes, workflow.Node{Name: op.Name(), Version: 1, Description: op.Description(), InputSchema: op.Schema(), OutputSchema: op.OutputSchema(), ReadOnly: op.ReadOnly(), RetrySafe: op.RetrySafe(), Resources: op.Resources, Execute: op.Run, Validate: op.Validate, ValidateTemplate: op.ValidateTemplate})
	}
	return nodes
}
