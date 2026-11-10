package nodes

import (
	"github.com/keshon/gektor/internal/capabilities/media"
	"github.com/keshon/gektor/internal/capabilities/media/contract"
	"github.com/keshon/gektor/internal/workflow"
)

func MediaDefinitions(svc *media.Service) []workflow.Node {
	result := []workflow.Node{}
	for _, name := range contract.Names() {
		op := contract.Operation{Service: svc, Kind: name}
		result = append(result, workflow.Node{Name: name, Version: 1, Description: op.Description(), InputSchema: op.Schema(), OutputSchema: op.OutputSchema(), ReadOnly: op.ReadOnly(), RetrySafe: op.RetrySafe(), Resources: op.Resources, Validate: op.Validate, Execute: op.Run,
			ValidateTemplate: op.ValidateTemplate,
		})
	}
	return result
}
