package nodes

import (
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/integrations/pixelita"
	"github.com/keshon/gektor/internal/workspace"
	"path/filepath"
	"strings"
)

func Definitions(ws *workspace.Workspace, output pixelita.OutputAuthorizer) []automation.Node {
	var nodes []automation.Node
	for _, name := range []string{"inspect", "preview", "variants", "compare"} {
		op := pixelita.Operation{WS: ws, Operation: name, AuthorizeOutput: output}
		resources := func(args json.RawMessage) ([]string, error) {
			paths, err := pixelita.Resources(args)
			if err != nil {
				return nil, err
			}
			for i, p := range paths {
				if ws == nil {
					return nil, fmt.Errorf("workspace unavailable")
				}
				rel, err := ws.Relative(p)
				if err != nil {
					return nil, err
				}
				paths[i] = filepath.ToSlash(rel)
			}
			return paths, nil
		}
		nodes = append(nodes, automation.Node{Name: op.Name(), Version: 1, Description: op.Description(), InputSchema: op.Schema(), OutputSchema: json.RawMessage(`{"type":"object","properties":{"schema":{"type":"string"},"tool":{"type":"string"},"summary":{"type":"object","additionalProperties":{"type":"integer"}},"status":{"type":"string"},"sources":{"type":["array","null"],"items":{"type":"object"}},"items":{"type":["array","null"],"items":{"type":"object"}},"recovery":{"type":"string"},"error":{"type":"string"},"failure_stage":{"type":"string"}},"required":["schema","tool","summary","status","sources"]}`), ReadOnly: op.ReadOnly(), Resources: resources, Execute: op.Run, Validate: func(args json.RawMessage) error { _, err := op.Validate(args); return err }, ValidateTemplate: func(args json.RawMessage) error { return literalPaths(ws, args) }})
	}
	return nodes
}
func literalPaths(ws *workspace.Workspace, args json.RawMessage) error {
	var in map[string]json.RawMessage
	_ = json.Unmarshal(args, &in)
	var paths []json.RawMessage
	_ = json.Unmarshal(in["paths"], &paths)
	paths = append(paths, in["out_dir"])
	for _, raw := range paths {
		var path string
		if json.Unmarshal(raw, &path) == nil && path != "" {
			for _, marker := range []string{"{$input", "{$step", "${input", "${step"} {
				if strings.Contains(path, marker) {
					return fmt.Errorf("reference-like path %q: use a JSON object reference", path)
				}
			}
			if ws == nil {
				return fmt.Errorf("workspace unavailable")
			}
			if _, err := ws.Relative(path); err != nil {
				return err
			}
		}
	}
	return nil
}
