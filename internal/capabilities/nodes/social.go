// Package nodes exposes the public-post reader to Workflows.
package nodes

import (
	"github.com/keshon/gektor/internal/capabilities/social"
	"github.com/keshon/gektor/internal/capabilities/social/contract"
	"github.com/keshon/gektor/internal/workflow"
)

func SocialDefinitions(s social.Service) []workflow.Node {
	op := contract.Operation{Service: s}
	return []workflow.Node{{Name: op.Name(), PermissionName: contract.AgentName, Version: 1, Description: op.Description(), ReadOnly: op.ReadOnly(), RetrySafe: op.RetrySafe(), InputSchema: op.Schema(), OutputSchema: op.OutputSchema(), Validate: op.Validate, Resources: op.Resources, Execute: op.Run}}
}
