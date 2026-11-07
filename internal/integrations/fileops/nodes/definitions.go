package nodes

import (
	"encoding/json"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/integrations/fileops"
	"github.com/keshon/gektor/internal/integrations/fileops/adapters"
	"github.com/keshon/gektor/internal/workspace"
)

func Definitions(ws *workspace.Workspace, authorize adapters.OutputAuthorizer) []automation.Node {
	var result []automation.Node
	for _, name := range []string{"download_file", "archive_list", "archive_create", "archive_extract"} {
		op := adapters.Operation{Service: fileops.Service{WS: ws}, Kind: name, AuthorizeOutput: authorize}
		result = append(result, automation.Node{Name: op.Name(), Version: 1, Description: op.Description(),
			InputSchema: op.Schema(), OutputSchema: op.OutputSchema(), ReadOnly: op.ReadOnly(), RetrySafe: op.ReadOnly(),
			Resources: op.Resources, Validate: op.Validate, Execute: op.Run,
			ValidateTemplate: func(raw json.RawMessage) error {
				var args map[string]json.RawMessage
				if err := json.Unmarshal(raw, &args); err != nil {
					return err
				}
				paths := []json.RawMessage{args["path"], args["out_dir"]}
				var sources []json.RawMessage
				_ = json.Unmarshal(args["paths"], &sources)
				paths = append(paths, sources...)
				for _, raw := range paths {
					var path string
					if json.Unmarshal(raw, &path) == nil && path != "" && ws != nil {
						if _, err := ws.Relative(path); err != nil {
							return err
						}
					}
				}
				return nil
			}})
	}
	return result
}
