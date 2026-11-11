package recipe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/workflow"
)

func CheckInput(raw json.RawMessage, kind string) error {
	if _, err := bind(raw, func(map[string]any) (any, error) {
		return nil, fmt.Errorf("input values cannot contain reserved $input or $step objects")
	}); err != nil {
		return err
	}

	v, err := jsonValue(raw, MaxArguments)
	if err != nil {
		return err
	}

	ok := false

	switch kind {
	case "string":
		_, ok = v.(string)
	case "boolean":
		_, ok = v.(bool)
	case "object":
		_, ok = v.(map[string]any)
	case "array":
		_, ok = v.([]any)
	case "number", "integer":
		n, number := v.(json.Number)
		ok = number
		if ok && kind == "integer" {
			// Decode preserves numbers; accept JSON integers without float rounding.
			_, err := strconv.ParseInt(string(n), 10, 64)
			ok = err == nil
		}
	}

	if !ok {
		return fmt.Errorf("expected %s", kind)
	}

	return nil
}

func jsonValue(raw []byte, limit int) (any, error) {
	if len(raw) > limit {
		return nil, fmt.Errorf("JSON value exceeds %d bytes", limit)
	}

	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}

	if d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("expected one JSON value")
	}

	return value, nil
}

// MapReferences transforms structured references without interpolating strings.
func MapReferences(raw json.RawMessage, lookup func(map[string]any) (any, error)) (json.RawMessage, error) {
	return bind(raw, lookup)
}

func bind(raw json.RawMessage, lookup func(map[string]any) (any, error)) (json.RawMessage, error) {
	v, err := jsonValue(raw, MaxArguments)
	if err != nil {
		return nil, err
	}

	var walk func(any, int) (any, error)
	walk = func(v any, depth int) (any, error) {
		if depth > 64 {
			return nil, fmt.Errorf("argument nesting exceeds 64 levels")
		}

		switch value := v.(type) {
		case map[string]any:
			_, input := value["$input"]
			_, output := value["$step"]
			if input || output {
				return lookup(value)
			}

			for key, child := range value {
				resolved, err := walk(child, depth+1)
				if err != nil {
					return nil, err
				}

				value[key] = resolved
			}
		case []any:
			for i, child := range value {
				resolved, err := walk(child, depth+1)
				if err != nil {
					return nil, err
				}

				value[i] = resolved
			}
		}

		return v, nil
	}
	v, err = walk(v, 0)
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(v)
	if err != nil || len(data) > MaxArguments {
		return nil, fmt.Errorf("resolved arguments exceed 64 KiB or are invalid")
	}

	return data, nil
}

// HasOutputReferences identifies templates whose values require earlier results.
func HasOutputReferences(args json.RawMessage) bool {
	found := false
	_, _ = bind(args, func(ref map[string]any) (any, error) {
		if _, ok := ref["$step"]; ok {
			found = true
		}

		return ref, nil
	})
	return found
}

func outputReference(ref map[string]any) (string, string, error) {
	name, ok := ref["$step"].(string)
	pointer := ""
	if p, exists := ref["pointer"]; exists {
		var valid bool
		pointer, valid = p.(string)
		if !valid {
			return "", "", fmt.Errorf("output pointer must be a string")
		}
	}

	if !ok || name == "" || len(ref) > 2 {
		return "", "", fmt.Errorf("output reference requires $step and optional pointer")
	}

	for key := range ref {
		if key != "$step" && key != "pointer" {
			return "", "", fmt.Errorf("unknown output reference field %q", key)
		}
	}

	return name, pointer, nil
}

func pointerParts(pointer string) ([]string, error) {
	if pointer == "" {
		return nil, nil
	}

	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("output pointer must be empty or begin with /")
	}

	parts := strings.Split(pointer[1:], "/")

	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] == '~' {
				if j+1 == len(part) || (part[j+1] != '0' && part[j+1] != '1') {
					return nil, fmt.Errorf("invalid output pointer escape")
				}

				j++
			}
		}

		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}

	return parts, nil
}

func ResolveOutputs(args json.RawMessage, outputs map[string]string) (json.RawMessage, error) {
	return bind(args, func(ref map[string]any) (any, error) {
		name, pointer, err := outputReference(ref)
		if err != nil {
			return nil, err
		}

		raw, ok := outputs[name]
		if !ok {
			return nil, fmt.Errorf("output %q unavailable", name)
		}

		value, err := outputAt([]byte(raw), pointer)
		if err != nil {
			return nil, fmt.Errorf("output %s: %w", name, err)
		}

		return value, nil
	})
}

// OutputAt selects a recorded output using the same JSON Pointer rules as
// recipe references, without imposing the smaller step-argument size limit.
func OutputAt(raw []byte, pointer string) (json.RawMessage, error) {
	value, err := outputAt(raw, pointer)
	if err != nil {
		return nil, err
	}

	return json.Marshal(value)
}

func outputAt(raw []byte, pointer string) (any, error) {
	value, err := jsonValue(raw, MaxOutput)
	if err != nil {
		return nil, err
	}

	parts, err := pointerParts(pointer)
	if err != nil {
		return nil, err
	}

	for _, part := range parts {
		ok := false

		switch node := value.(type) {
		case map[string]any:
			value, ok = node[part]
		case []any:
			i, err := strconv.Atoi(part)
			ok = err == nil && i >= 0 && i < len(node) && strconv.Itoa(i) == part
			if ok {
				value = node[i]
			}
		}

		if !ok {
			return nil, fmt.Errorf("no value at %q", pointer)
		}
	}

	return value, nil
}

// BindInputs validates a concrete invocation against its declared typed inputs.
func BindInputs(specs map[string]workflow.Input, values map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if len(specs) > 32 {
		return nil, fmt.Errorf("workflow permits at most 32 inputs")
	}

	bound := make(map[string]json.RawMessage, len(specs))

	for name := range values {
		if _, ok := specs[name]; !ok {
			return nil, fmt.Errorf("unknown input %q", name)
		}
	}

	for name, spec := range specs {
		if name == "" || len(name) > 64 {
			return nil, fmt.Errorf("input names must contain 1–64 bytes")
		}

		switch spec.Type {
		case "string", "integer", "number", "boolean", "array", "object":
		default:
			return nil, fmt.Errorf("input %s: unsupported type %q", name, spec.Type)
		}

		if spec.Default != nil {
			if err := CheckInput(spec.Default, spec.Type); err != nil {
				return nil, fmt.Errorf("input %s default: %w", name, err)
			}
		}

		value, exists := values[name]
		if !exists {
			value = spec.Default
		}

		if value == nil {
			if spec.Required {
				return nil, fmt.Errorf("required input %q missing", name)
			}

			continue
		}

		if err := CheckInput(value, spec.Type); err != nil {
			return nil, fmt.Errorf("input %s: %w", name, err)
		}

		bound[name] = append(json.RawMessage(nil), value...)
	}

	data, err := json.Marshal(bound)
	if err != nil || len(data) > MaxArguments {
		return nil, fmt.Errorf("bound inputs exceed 64 KiB or are invalid")
	}

	return bound, nil
}
