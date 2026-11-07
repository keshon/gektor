// Package nodes exposes document readers as workflow nodes.
package nodes

import (
	"encoding/json"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/integrations/documents"
	"github.com/keshon/gektor/internal/integrations/documents/adapters"
	"github.com/keshon/gektor/internal/workspace"
)

func Definitions(ws *workspace.Workspace) []automation.Node {
	var result []automation.Node
	for _, kind := range []string{"docx_read", "xlsx_read", "pdf_read"} {
		op := adapters.Operation{Service: documents.Service{WS: ws}, Kind: kind}
		result = append(result, automation.Node{Name: kind, Version: 1, Description: op.Description(), InputSchema: op.Schema(), OutputSchema: op.OutputSchema(), ReadOnly: true, RetrySafe: true, Validate: op.Validate, Resources: op.Resources, Execute: op.Run,
			ValidateTemplate: func(raw json.RawMessage) error {
				var args map[string]json.RawMessage
				if err := json.Unmarshal(raw, &args); err != nil {
					return err
				}
				var name string
				if json.Unmarshal(args["path"], &name) == nil && name != "" && ws != nil {
					_, err := ws.Relative(name)
					return err
				}
				return nil
			}})
	}
	return result
}
