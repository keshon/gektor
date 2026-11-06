package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/agent"
)

// templateTool allows output references where an operation expects a value.
// Concrete values still pass through the original schema and rooted path checks.
type templateTool struct{ agent.Tool }

func (t templateTool) Schema() json.RawMessage {
	var schema map[string]any
	_ = json.Unmarshal(t.Tool.Schema(), &schema)
	ref := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"$step":   map[string]any{"type": "string", "minLength": 1},
			"pointer": map[string]any{"type": "string"},
		},
		"required": []string{"$step"}, "additionalProperties": false,
	}
	var decorate func(map[string]any) map[string]any
	decorate = func(s map[string]any) map[string]any {
		if properties, ok := s["properties"].(map[string]any); ok {
			for key, value := range properties {
				properties[key] = decorate(value.(map[string]any))
			}
		}
		if items, ok := s["items"].(map[string]any); ok {
			s["items"] = decorate(items)
		}
		return map[string]any{"anyOf": []any{s, ref}}
	}
	data, _ := json.Marshal(decorate(schema))
	return data
}

func (h handler) validateTemplate(t agent.Tool, args json.RawMessage) error {
	return agent.NewRegistry(templateTool{t}).ValidateCall(t.Name(), args)
}

func (h handler) validateLiteralPaths(args json.RawMessage) error {
	// Check literal paths now, even when a different argument is dynamic.
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
					return fmt.Errorf("reference-like path %q: use a JSON object such as {\"$input\":\"source\"}, not string interpolation", path)
				}
			}
			if _, err := h.ws.Relative(path); err != nil {
				return err
			}
		}
	}
	return nil
}
