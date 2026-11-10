// Package nodes exposes document readers as workflow nodes.
package nodes

import (
	"github.com/keshon/gektor/internal/capabilities/documents"
	"github.com/keshon/gektor/internal/capabilities/documents/contract"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workspace"
)

func DocumentDefinitions(ws *workspace.Workspace) []workflow.Node {
	var result []workflow.Node
	for _, kind := range contract.Names() {
		op := contract.Operation{Service: documents.Service{WS: ws}, Kind: kind}
		result = append(result, workflow.Node{Name: kind, Version: 1, Description: op.Description(), InputSchema: op.Schema(), OutputSchema: op.OutputSchema(), ReadOnly: op.ReadOnly(), RetrySafe: op.RetrySafe(), Validate: op.Validate, Resources: op.Resources, Execute: op.Run,
			ValidateTemplate: op.ValidateTemplate})
	}
	return result
}
