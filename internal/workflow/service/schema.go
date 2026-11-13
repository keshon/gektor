package service

import "encoding/json"

// DefinitionSchema describes recipes using the registered node contracts.
func (s Service) DefinitionSchema() json.RawMessage {
	input := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":     map[string]any{"type": "string", "enum": []string{"string", "integer", "number", "boolean", "array", "object"}},
			"required": map[string]any{"type": "boolean"},
			"default":  map[string]any{},
		},
		"required":             []string{"type"},
		"additionalProperties": false,
	}
	retry := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"max_attempts": map[string]any{"type": "integer", "minimum": 1, "maximum": 3},
			"delay_ms":     map[string]any{"type": "integer", "minimum": 0, "maximum": 10000},
		},
		"required":             []string{"max_attempts"},
		"additionalProperties": false,
	}
	repeat := map[string]any{
		"type":        "object",
		"description": "Serial workflow_call loop. Until selects a boolean from child output; next_inputs selects the next child input object. The last child output is the step result. Limit exhaustion fails. Delays occupy the current execution slot, not a durable wait.",
		"properties": map[string]any{
			"max_iterations": map[string]any{"type": "integer", "minimum": 1, "maximum": 10000},
			"until":          map[string]any{"type": "string"},
			"next_inputs":    map[string]any{"type": "string"},
			"delay_ms":       map[string]any{"type": "integer", "minimum": 0, "maximum": 10000},
		},
		"required":             []string{"max_iterations", "until", "next_inputs"},
		"additionalProperties": false,
	}
	step := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":           map[string]any{"type": "string", "minLength": 1, "maxLength": 64, "pattern": `^[^@/][^/]*$`},
			"tool":         map[string]any{"type": "string", "enum": s.Registry.Names()},
			"args":         map[string]any{"type": "object"},
			"review":       map[string]any{"type": "boolean"},
			"for_each":     map[string]any{"description": "Array or reference resolving to an array. Ordinary nodes require args={} and complete argument objects per item. workflow_call keeps a literal recipe_path in args and uses child input objects per item."},
			"concurrency":  map[string]any{"type": "integer", "minimum": 1, "maximum": 32},
			"on_error":     map[string]any{"type": "string", "enum": []string{"stop", "collect"}},
			"node_version": map[string]any{"type": "integer", "minimum": 1},
			"retry":        retry,
			"repeat":       repeat,
			"depends_on":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "uniqueItems": true},
			"when":         map[string]any{},
			"merge":        map[string]any{"type": "string", "enum": []string{"all", "any"}},
		},
		"required":             []string{"id", "tool", "args"},
		"additionalProperties": false,
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"version": map[string]any{"type": "integer", "const": 4},
			"name":    map[string]any{"type": "string", "minLength": 1, "maxLength": 120},
			"inputs":  map[string]any{"type": "object", "maxProperties": 32, "additionalProperties": input},
			"steps":   map[string]any{"type": "array", "minItems": 1, "maxItems": 16, "items": step},
			"outputs": map[string]any{
				"type": "object", "maxProperties": 32,
				"description": "Named values returned by this recipe. Required for recipes used by workflow_call. Values may use $input and $step references.",
			},
		},
		"required":             []string{"version", "name", "steps"},
		"additionalProperties": false,
	}
	// All values above are JSON primitives, slices, or maps with string keys.
	data, _ := json.Marshal(schema)
	return data
}
