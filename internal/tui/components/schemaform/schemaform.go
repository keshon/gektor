// Package schemaform maps JSON object schemas to the shared form component.
// Unsupported structures remain editable as JSON rather than being discarded.
package schemaform

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/keshon/gektor/internal/tui/components/form"
)

type Property struct {
	Type        json.RawMessage   `json:"type"`
	Description string            `json:"description"`
	Default     json.RawMessage   `json:"default"`
	Enum        []json.RawMessage `json:"enum"`
}
type Schema struct {
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required"`
}

func (p Property) Kind() string        { var s string; _ = json.Unmarshal(p.Type, &s); return s }
func Parse(raw json.RawMessage) Schema { var s Schema; _ = json.Unmarshal(raw, &s); return s }
func (s Schema) Fields(raw json.RawMessage) []form.Field {
	var values map[string]json.RawMessage
	_ = json.Unmarshal(raw, &values)
	keys := []string{}

	for k := range s.Properties {
		keys = append(keys, k)
	}

	sort.Strings(keys)
	fields := []form.Field{}

	for _, k := range keys {
		p := s.Properties[k]
		v := values[k]
		label := k
		if p.Kind() != "string" {
			label += " (JSON)"
		}

		for _, r := range s.Required {
			if r == k {
				label += " *"
			}
		}

		value := string(v)
		if p.Kind() == "string" && len(v) > 0 {
			var plain string
			if json.Unmarshal(v, &plain) == nil {
				value = plain
			}
		}

		fields = append(fields, form.Field{
			Key:       k,
			Label:     label,
			Value:     value,
			Multiline: p.Kind() == "object" || p.Kind() == "array" || strings.Contains(value, "\n"),
		})
	}

	if len(fields) == 0 {
		fields = append(fields, form.Field{
			Key:       "$json",
			Label:     "Arguments (JSON)",
			Value:     string(raw),
			Multiline: true,
		})
	}

	return fields
}
func (s Schema) Values(original json.RawMessage, values map[string]string) (json.RawMessage, error) {
	if v, ok := values["$json"]; ok {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(v), &obj); err != nil {
			return nil, err
		}

		if obj == nil {
			return nil, fmt.Errorf("arguments must be an object")
		}

		return json.RawMessage(v), nil
	}

	var obj map[string]json.RawMessage
	_ = json.Unmarshal(original, &obj)
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}

	for k, p := range s.Properties {
		v := values[k]
		required := false

		for _, r := range s.Required {
			if r == k {
				required = true
			}
		}

		if v == "" && !required {
			delete(obj, k)
			continue
		}

		if p.Kind() == "string" {
			var ref map[string]json.RawMessage
			isRef := json.Unmarshal([]byte(v), &ref) == nil && (ref["$step"] != nil || ref["$input"] != nil)
			if !isRef {
				obj[k], _ = json.Marshal(v)
				continue
			}
		}

		if !json.Valid([]byte(v)) {
			return nil, fmt.Errorf("%s: enter valid JSON", k)
		}

		obj[k] = json.RawMessage(v)
	}

	return json.Marshal(obj)
}
func (s Schema) Choices() map[string][]string {
	out := map[string][]string{}

	for k, p := range s.Properties {
		if len(p.Enum) == 0 {
			continue
		}

		for _, v := range p.Enum {
			value := string(v)
			if p.Kind() == "string" {
				_ = json.Unmarshal(v, &value)
			}

			out[k] = append(out[k], value)
		}
	}

	return out
}
