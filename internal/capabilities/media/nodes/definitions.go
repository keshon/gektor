package nodes

import (
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/capabilities/media"
	"github.com/keshon/gektor/internal/capabilities/media/contract"
)

func Definitions(svc *media.Service) []automation.Node {
	result := []automation.Node{}
	for _, name := range contract.Names() {
		op := contract.Operation{Service: svc, Kind: name}
		result = append(result, automation.Node{Name: name, Version: 1, Description: op.Description(), InputSchema: op.Schema(), OutputSchema: op.OutputSchema(), ReadOnly: op.ReadOnly(), RetrySafe: op.RetrySafe(), Resources: op.Resources, Validate: op.Validate, Execute: op.Run,
			ValidateTemplate: op.ValidateTemplate,
		})
	}
	return result
}
