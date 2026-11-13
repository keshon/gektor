// Package recipe validates recipe structure, binds inputs and resolves data references.
package recipe

import (
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/workflow"
)

const MaxArguments = 64 << 10

const MaxOutput = 1 << 20

// ValidateTemplate checks a scope before its invocation inputs are available.
// Input references become unknown outputs only in this temporary validation graph.
func ValidateTemplate(d workflow.Definition, h workflow.Handler) error {
	specs := make(map[string]workflow.Input, len(d.Inputs))
	for name, spec := range d.Inputs {
		spec.Required = false
		specs[name] = spec
	}
	_, err := BindInputs(specs, nil)
	if err != nil {
		return err
	}
	if len(d.Inputs) == 0 {
		_, _, err = Prepare(d, nil, h)
		return err
	}
	steps := make([]workflow.Step, 0, len(d.Steps)+1)
	steps = append(steps, workflow.Step{ID: "@inputs", Tool: "workflow_value", Args: json.RawMessage(`{}`)})
	for _, s := range d.Steps {
		convert := func(ref map[string]any) (any, error) {
			name, input := ref["$input"]
			if !input {
				return ref, nil
			}
			key, ok := name.(string)
			if !ok || len(ref) != 1 {
				return nil, fmt.Errorf("invalid input reference")
			}
			if _, ok = d.Inputs[key]; !ok {
				return nil, fmt.Errorf("unknown input %q", key)
			}
			return map[string]any{"$step": "@inputs"}, nil
		}
		s.Args, err = MapReferences(s.Args, convert)
		if err == nil && len(s.ForEach) > 0 {
			s.ForEach, err = MapReferences(s.ForEach, convert)
		}
		if err == nil && len(s.When) > 0 {
			s.When, err = MapReferences(s.When, convert)
		}
		if err != nil {
			return err
		}
		steps = append(steps, s)
	}
	d.Steps, d.Inputs = steps, nil
	_, _, err = Prepare(d, nil, h)
	return err
}

// Prepare binds inputs and checks references before any effects. Output values
// are unavailable here; handlers validate their argument templates separately.
func Prepare(d workflow.Definition, values map[string]json.RawMessage, h workflow.Handler) (workflow.Definition, map[string]json.RawMessage, error) {
	if d.Version != 4 {
		return d, nil, fmt.Errorf("workflow version must be 4")
	}

	ordered, err := Order(d.Steps)
	if err != nil {
		return d, nil, err
	}

	d.Steps = ordered
	if len(d.Name) == 0 || len(d.Name) > 120 || len(d.Steps) == 0 || len(d.Steps) > 128 {
		return d, nil, fmt.Errorf("workflow requires a 1–120 byte name and 1–128 scope steps")
	}

	bound, err := BindInputs(d.Inputs, values)
	if err != nil {
		return d, nil, err
	}

	steps := make([]workflow.Step, len(d.Steps))
	seen := map[string]bool{}

	for i, s := range d.Steps {
		if s.ID == "" || len(s.ID) > 64 || seen[s.ID] {
			return d, nil, fmt.Errorf("step IDs must be unique and contain 1–64 bytes")
		}

		{
			bindInput := func(ref map[string]any) (any, error) {
				if name, ok := ref["$input"]; ok {
					key, ok := name.(string)
					if !ok || len(ref) != 1 {
						return nil, fmt.Errorf("input reference must contain only a string $input")
					}

					value, ok := bound[key]
					if !ok {
						return nil, fmt.Errorf("input %q has no value", key)
					}

					return jsonValue(value, MaxArguments)
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
			}
			s.Args, err = bind(s.Args, bindInput)
			if err == nil && len(s.ForEach) > 0 {
				s.ForEach, err = bind(s.ForEach, bindInput)
			}

			if err == nil && len(s.When) > 0 {
				s.When, err = bind(s.When, bindInput)
			}

			if err != nil {
				return d, nil, fmt.Errorf("step %s: %w", s.ID, err)
			}
		}
		if len(s.When) > 0 && !HasOutputReferences(s.When) {
			var value bool
			if err := json.Unmarshal(s.When, &value); err != nil || string(s.When) == "null" {
				return d, nil, fmt.Errorf("step %s condition must be boolean", s.ID)
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
