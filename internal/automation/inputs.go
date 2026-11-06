package automation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Input declares a version-2 workflow parameter. Values are JSON, not expressions.
type Input struct {
	Type     string          `json:"type"`
	Required bool            `json:"required,omitempty"`
	Default  json.RawMessage `json:"default,omitempty"`
}

const maxArguments = 64 << 10
const maxOutput = 1 << 20

// Prepare binds inputs and checks references before any effects. Output values
// are unavailable here; handlers validate their argument templates separately.
func Prepare(d Definition, values map[string]json.RawMessage, h Handler) (Definition, map[string]json.RawMessage, error) {
	if d.Version != 1 && d.Version != 2 {
		return d, nil, fmt.Errorf("workflow version must be 1 or 2")
	}
	if d.Version == 1 && (len(d.Inputs) != 0 || len(values) != 0) {
		return d, nil, fmt.Errorf("inputs require workflow version 2")
	}
	if len(d.Name) == 0 || len(d.Name) > 120 || len(d.Steps) == 0 || len(d.Steps) > 16 {
		return d, nil, fmt.Errorf("workflow requires a 1–120 byte name and 1–16 steps")
	}
	if len(d.Inputs) > 32 {
		return d, nil, fmt.Errorf("workflow permits at most 32 inputs")
	}
	bound := make(map[string]json.RawMessage, len(d.Inputs))
	for name := range values {
		if _, ok := d.Inputs[name]; !ok {
			return d, nil, fmt.Errorf("unknown input %q", name)
		}
	}
	for name, spec := range d.Inputs {
		if name == "" || len(name) > 64 {
			return d, nil, fmt.Errorf("input names must contain 1–64 bytes")
		}
		switch spec.Type {
		case "string", "integer", "number", "boolean", "array", "object":
		default:
			return d, nil, fmt.Errorf("input %s: unsupported type %q", name, spec.Type)
		}
		if spec.Default != nil {
			if err := checkInput(spec.Default, spec.Type); err != nil {
				return d, nil, fmt.Errorf("input %s default: %w", name, err)
			}
		}
		value, exists := values[name]
		if !exists {
			value = spec.Default
		}
		if value == nil {
			if spec.Required {
				return d, nil, fmt.Errorf("required input %q missing", name)
			}
			continue
		}
		if err := checkInput(value, spec.Type); err != nil {
			return d, nil, fmt.Errorf("input %s: %w", name, err)
		}
		bound[name] = append(json.RawMessage(nil), value...)
	}
	data, err := json.Marshal(bound)
	if err != nil || len(data) > maxArguments {
		return d, nil, fmt.Errorf("bound inputs exceed 64 KiB or are invalid")
	}
	steps := make([]Step, len(d.Steps))
	seen := map[string]bool{}
	for i, s := range d.Steps {
		if s.ID == "" || len(s.ID) > 64 || seen[s.ID] {
			return d, nil, fmt.Errorf("step IDs must be unique and contain 1–64 bytes")
		}
		if d.Version == 2 {
			s.Args, err = bind(s.Args, func(ref map[string]any) (any, error) {
				if name, ok := ref["$input"]; ok {
					key, ok := name.(string)
					if !ok || len(ref) != 1 {
						return nil, fmt.Errorf("input reference must contain only a string $input")
					}
					value, ok := bound[key]
					if !ok {
						return nil, fmt.Errorf("input %q has no value", key)
					}
					return jsonValue(value, maxArguments)
				}
				name, pointer, err := outputReference(ref)
				if err != nil {
					return nil, err
				}
				if !seen[name] {
					return nil, fmt.Errorf("output reference %q must name an earlier step", name)
				}
				if _, err := pointerParts(pointer); err != nil {
					return nil, err
				}
				return ref, nil
			})
			if err != nil {
				return d, nil, fmt.Errorf("step %s: %w", s.ID, err)
			}
		}
		if err := h.Validate(s); err != nil {
			return d, nil, fmt.Errorf("step %s: %w", s.ID, err)
		}
		seen[s.ID] = true
		steps[i] = s
	}
	d.Steps = steps
	return d, bound, nil
}

func checkInput(raw json.RawMessage, kind string) error {
	if _, err := bind(raw, func(map[string]any) (any, error) {
		return nil, fmt.Errorf("input values cannot contain reserved $input or $step objects")
	}); err != nil {
		return err
	}
	v, err := jsonValue(raw, maxArguments)
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

func bind(raw json.RawMessage, lookup func(map[string]any) (any, error)) (json.RawMessage, error) {
	v, err := jsonValue(raw, maxArguments)
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
	if err != nil || len(data) > maxArguments {
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

func resolveOutputs(args json.RawMessage, outputs map[string]string) (json.RawMessage, error) {
	return bind(args, func(ref map[string]any) (any, error) {
		name, pointer, err := outputReference(ref)
		if err != nil {
			return nil, err
		}
		raw, ok := outputs[name]
		if !ok {
			return nil, fmt.Errorf("output %q unavailable", name)
		}
		value, err := jsonValue([]byte(raw), maxOutput)
		if err != nil {
			return nil, fmt.Errorf("output %s: %w", name, err)
		}
		parts, err := pointerParts(pointer)
		if err != nil {
			return nil, err
		}
		for _, part := range parts {
			switch node := value.(type) {
			case map[string]any:
				value, ok = node[part]
			case []any:
				i, e := strconv.Atoi(part)
				ok = e == nil && i >= 0 && i < len(node) && strconv.Itoa(i) == part
				if ok {
					value = node[i]
				}
			default:
				ok = false
			}
			if !ok {
				return nil, fmt.Errorf("output %s has no value at %q", name, pointer)
			}
		}
		return value, nil
	})
}
