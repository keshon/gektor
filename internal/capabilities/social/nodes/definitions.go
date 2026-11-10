// Package nodes exposes the public-post reader to Workflows.
package nodes

import (
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/capabilities/social"
	"github.com/keshon/gektor/internal/capabilities/social/contract"
)

func Definitions(s social.Service) []automation.Node {
	op := contract.Operation{Service: s}
	return []automation.Node{{Name: op.Name(), PermissionName: contract.AgentName, Version: 1, Description: op.Description(), ReadOnly: true, RetrySafe: true, InputSchema: op.Schema(), OutputSchema: op.OutputSchema(), Validate: op.Validate, Resources: op.Resources, Execute: op.Run}}
}
