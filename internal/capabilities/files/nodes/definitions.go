// Package nodes exposes native file operations as workflow nodes.
package nodes

import (
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/capabilities/files"
	"github.com/keshon/gektor/internal/capabilities/files/contract"
	"github.com/keshon/gektor/internal/workspace"
)

func Definitions(ws *workspace.Workspace, authorize contract.OutputAuthorizer) []automation.Node {
	var result []automation.Node
	for _, name := range contract.Names() {
		op := contract.Operation{Service: files.Service{WS: ws}, Kind: name, AuthorizeOutput: authorize}
		result = append(result, automation.Node{Name: op.Name(), Version: 1, Description: op.Description(),
			InputSchema: op.Schema(), OutputSchema: op.OutputSchema(), ReadOnly: op.ReadOnly(), RetrySafe: op.RetrySafe(),
			Resources: op.Resources, Validate: op.Validate, Execute: op.Run,
			ValidateTemplate: op.ValidateTemplate})
	}
	return result
}
