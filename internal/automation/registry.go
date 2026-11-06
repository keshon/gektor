package automation

import (
	"encoding/json"
	"fmt"
	"sort"
)

type Registry struct{ nodes map[string]Node }

func NewRegistry(nodes ...Node) (*Registry, error) {
	r := &Registry{nodes: map[string]Node{}}
	for _, n := range nodes {
		if n.Name == "" || n.Version < 1 || n.Execute == nil || n.Resources == nil {
			return nil, fmt.Errorf("node requires name, version, executor and resources")
		}
		if _, ok := r.nodes[n.Name]; ok {
			return nil, fmt.Errorf("duplicate node %s", n.Name)
		}
		if _, err := CompileSchema(n.InputSchema); err != nil {
			return nil, fmt.Errorf("node %s: %w", n.Name, err)
		}
		if len(n.OutputSchema) > 0 {
			if _, err := CompileSchema(n.OutputSchema); err != nil {
				return nil, fmt.Errorf("node %s output: %w", n.Name, err)
			}
		}
		n.InputSchema = append(json.RawMessage(nil), n.InputSchema...)
		n.OutputSchema = append(json.RawMessage(nil), n.OutputSchema...)
		r.nodes[n.Name] = n
	}
	return r, nil
}
func (r *Registry) Node(name string) (Node, error) {
	if r == nil {
		return Node{}, fmt.Errorf("node registry unavailable")
	}
	n, ok := r.nodes[name]
	if !ok {
		return Node{}, fmt.Errorf("unsupported workflow tool %q", name)
	}
	n.InputSchema = append(json.RawMessage(nil), n.InputSchema...)
	n.OutputSchema = append(json.RawMessage(nil), n.OutputSchema...)
	return n, nil
}
func (r *Registry) Names() []string {
	names := []string{}
	if r != nil {
		for name := range r.nodes {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
func (r *Registry) Schemas() map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, name := range r.Names() {
		n, _ := r.Node(name)
		out[name] = n.InputSchema
	}
	return out
}

// TemplateSchema permits output references while retaining ordinary constraints.
func TemplateSchema(raw json.RawMessage) json.RawMessage {
	var schema map[string]any
	if json.Unmarshal(raw, &schema) != nil {
		return raw
	}
	ref := map[string]any{"type": "object", "properties": map[string]any{"$step": map[string]any{"type": "string", "minLength": 1}, "pointer": map[string]any{"type": "string"}}, "required": []string{"$step"}, "additionalProperties": false}
	var decorate func(map[string]any) map[string]any
	decorate = func(s map[string]any) map[string]any {
		if props, ok := s["properties"].(map[string]any); ok {
			for k, v := range props {
				if child, ok := v.(map[string]any); ok {
					props[k] = decorate(child)
				}
			}
		}
		if child, ok := s["items"].(map[string]any); ok {
			s["items"] = decorate(child)
		}
		return map[string]any{"anyOf": []any{s, ref}}
	}
	data, _ := json.Marshal(decorate(schema))
	return data
}
