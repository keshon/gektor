package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/recipe"
)

const maxCompositionDepth = 4
const maxExpandedSteps = 128

type recipeBundle struct {
	Root    json.RawMessage            `json:"root"`
	Recipes map[string]json.RawMessage `json:"recipes"`
}
type callInput struct {
	Path   string                     `json:"recipe_path"`
	Inputs map[string]json.RawMessage `json:"inputs"`
}

// SnapshotRecipe freezes the dependency closure at submission, not worker start.
// Snapshot remains the ordinary file reader used for inputs and schema files.
func (s Service) SnapshotRecipe(ctx context.Context, path string) ([]byte, error) {
	raw, err := s.Snapshot(ctx, path)
	if err != nil {
		return nil, err
	}
	return s.FreezeRecipe(ctx, raw, path)
}

// FreezeRecipe captures referenced recipes for an already loaded draft without executing it.
func (s Service) FreezeRecipe(ctx context.Context, raw []byte, path string) ([]byte, error) {
	b := recipeBundle{Root: raw, Recipes: map[string]json.RawMessage{}}
	total := len(raw)
	visitedSteps := 0
	var walk func([]byte, string, int, map[string]bool) error
	walk = func(source []byte, path string, depth int, stack map[string]bool) error {
		if depth > maxCompositionDepth {
			return fmt.Errorf("subworkflow nesting exceeds %d", maxCompositionDepth)
		}
		if stack[path] {
			return fmt.Errorf("subworkflow cycle at %s", path)
		}
		stack[path] = true
		defer delete(stack, path)
		if err := automation.ValidateJSON(source, s.DefinitionSchema()); err != nil {
			return fmt.Errorf("recipe %s: %w", path, err)
		}
		var d automation.Definition
		if err := json.Unmarshal(source, &d); err != nil {
			return err
		}
		visitedSteps += len(d.Steps)
		if visitedSteps > maxExpandedSteps {
			return fmt.Errorf("expanded workflow exceeds %d steps", maxExpandedSteps)
		}
		for _, step := range d.Steps {
			if step.Tool != "workflow_call" {
				continue
			}
			var in callInput
			if err := decode(step.Args, &in); err != nil {
				return fmt.Errorf("workflow_call requires a literal recipe_path: %w", err)
			}
			p, err := s.WS.Relative(in.Path)
			if err != nil {
				return err
			}
			p = filepath.ToSlash(p)
			child, ok := b.Recipes[p]
			if !ok {
				child, err = s.Snapshot(ctx, p)
				if err != nil {
					return err
				}
				total += len(child)
				if total > 64<<10 {
					return fmt.Errorf("recipe dependency closure exceeds 64 KiB")
				}
				b.Recipes[p] = child
			}
			if err := walk(child, p, depth+1, stack); err != nil {
				return err
			}
		}
		return nil
	}
	p, err := s.WS.Relative(path)
	if err != nil {
		return nil, err
	}
	if err := walk(raw, filepath.ToSlash(p), 0, map[string]bool{}); err != nil {
		return nil, err
	}
	if len(b.Recipes) == 0 {
		return raw, nil
	}
	data, err := json.Marshal(b)
	if len(data) > 64<<10 {
		return nil, fmt.Errorf("recipe bundle exceeds 64 KiB")
	}
	return data, err
}

func (s Service) compileSource(ctx context.Context, source []byte, path string) (automation.Definition, []byte, error) {
	var zero automation.Definition
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(source, &fields); err != nil {
		return zero, nil, err
	}
	b := recipeBundle{}
	if fields["root"] != nil {
		if err := decode(source, &b); err != nil {
			return zero, nil, err
		}
	} else {
		frozen, err := s.FreezeRecipe(ctx, source, path)
		if err != nil {
			return zero, nil, err
		}
		if bytes.Equal(frozen, source) {
			b.Root = source
			b.Recipes = map[string]json.RawMessage{}
		} else {
			if err := decode(frozen, &b); err != nil {
				return zero, nil, err
			}
		}
		source = frozen
	}
	if len(source) > 64<<10 {
		return zero, nil, fmt.Errorf("recipe bundle exceeds 64 KiB")
	}
	// Ordinary recipes keep their original execution definition and digest.
	// Composition must not rewrite argument ordering or inferred graph edges
	// for runs that do not use it (the evaluation verifier checks this binding).
	if len(b.Recipes) == 0 {
		if err := automation.ValidateJSON(b.Root, s.DefinitionSchema()); err != nil {
			return zero, nil, err
		}
		var plain automation.Definition
		if err := json.Unmarshal(b.Root, &plain); err != nil {
			return zero, nil, err
		}
		calls := false
		for _, step := range plain.Steps {
			if strings.Contains(step.ID, "/") || strings.HasPrefix(step.ID, "@") {
				return zero, nil, fmt.Errorf("step ID %q uses a reserved composition namespace", step.ID)
			}
			calls = calls || step.Tool == "workflow_call"
		}
		if !calls && len(plain.Outputs) == 0 {
			return plain, source, nil
		}
	}
	for p := range b.Recipes {
		rel, err := s.WS.Relative(p)
		if err != nil || filepath.ToSlash(rel) != p {
			return zero, nil, fmt.Errorf("invalid bundled recipe path %q", p)
		}
		args, _ := json.Marshal(map[string]string{"path": p})
		if s.Authorize == nil {
			return zero, nil, fmt.Errorf("recipe read authorization unavailable")
		}
		if err := s.Authorize(ctx, "read_file", args); err != nil {
			return zero, nil, err
		}
	}
	count := 0
	boundNames := map[string]map[string]bool{}
	var expand func(json.RawMessage, string, string, string, int, map[string]bool) (automation.Definition, error)
	expand = func(raw json.RawMessage, path, prefix, binding string, depth int, stack map[string]bool) (automation.Definition, error) {
		if depth > maxCompositionDepth || stack[path] {
			return zero, fmt.Errorf("subworkflow cycle or nesting exceeds %d at %s", maxCompositionDepth, path)
		}
		stack[path] = true
		defer delete(stack, path)
		if err := automation.ValidateJSON(raw, s.DefinitionSchema()); err != nil {
			return zero, err
		}
		var d automation.Definition
		if err := json.Unmarshal(raw, &d); err != nil {
			return zero, err
		}
		ordered, err := recipe.Order(d.Steps)
		if err != nil {
			return zero, err
		}
		d.Steps = ordered
		for _, step := range d.Steps {
			if strings.Contains(step.ID, "/") || strings.HasPrefix(step.ID, "@") {
				return zero, fmt.Errorf("step ID %q uses a reserved composition namespace", step.ID)
			}
		}
		steps := []automation.Step{}
		for _, step := range d.Steps {
			id := prefix + step.ID
			rewrite := func(raw json.RawMessage) (json.RawMessage, error) {
				if binding != "" {
					if err := checkBoundNames(raw, boundNames[binding]); err != nil {
						return nil, err
					}
				}
				return rewriteScope(raw, prefix, binding)
			}
			var err error
			step.Args, err = rewrite(step.Args)
			if err != nil {
				return zero, err
			}
			if len(step.When) > 0 {
				step.When, err = rewrite(step.When)
				if err != nil {
					return zero, err
				}
			}
			if len(step.ForEach) > 0 {
				step.ForEach, err = rewrite(step.ForEach)
				if err != nil {
					return zero, err
				}
			}
			for i, dep := range step.DependsOn {
				step.DependsOn[i] = prefix + dep
			}
			if binding != "" {
				step.Gate = binding
				step.DependsOn = append(step.DependsOn, binding)
			}
			step.ID = id
			if step.Tool != "workflow_call" {
				steps = append(steps, step)
				count++
			} else {
				if len(step.ForEach) > 0 || step.Retry != nil {
					return zero, fmt.Errorf("workflow_call %s does not support for_each or retry", id)
				}
				if step.NodeVersion > 1 {
					return zero, fmt.Errorf("workflow_call version unavailable")
				}
				var in callInput
				if err := decode(step.Args, &in); err != nil {
					return zero, err
				}
				p, err := s.WS.Relative(in.Path)
				if err != nil {
					return zero, err
				}
				p = filepath.ToSlash(p)
				child, ok := b.Recipes[p]
				if !ok {
					return zero, fmt.Errorf("missing bundled recipe %s", p)
				}
				var definition automation.Definition
				if err := json.Unmarshal(child, &definition); err != nil {
					return zero, err
				}
				if len(definition.Outputs) == 0 {
					return zero, fmt.Errorf("called recipe %s must declare outputs", p)
				}
				bindID := id + "/@inputs"
				if in.Inputs == nil {
					in.Inputs = map[string]json.RawMessage{}
				}
				if definition.Inputs == nil {
					definition.Inputs = map[string]automation.Input{}
				}
				boundNames[bindID] = map[string]bool{}
				for name, spec := range definition.Inputs {
					_, provided := in.Inputs[name]
					boundNames[bindID][name] = provided || spec.Default != nil
				}
				args, _ := json.Marshal(map[string]any{"recipe_path": p, "specs": definition.Inputs, "values": in.Inputs})
				gate := step
				gate.ID = bindID
				gate.Tool = "workflow_bind"
				gate.Args = args
				steps = append(steps, gate)
				count++
				expanded, err := expand(child, p, id+"/", bindID, depth+1, stack)
				if err != nil {
					return zero, err
				}
				steps = append(steps, expanded.Steps...)
				deps := []string{bindID}
				for _, s := range expanded.Steps {
					deps = append(deps, s.ID)
				}
				values, _ := json.Marshal(expanded.Outputs)
				steps = append(steps, automation.Step{ID: id, Tool: "workflow_value", Args: values, DependsOn: deps, Gate: bindID, CollectOutputs: true})
				count++
			}
			if count > maxExpandedSteps {
				return zero, fmt.Errorf("expanded workflow exceeds %d steps", maxExpandedSteps)
			}
		}
		for key, raw := range d.Outputs {
			if binding != "" {
				if err := checkBoundNames(raw, boundNames[binding]); err != nil {
					return zero, err
				}
			}
			value, err := rewriteScope(raw, prefix, binding)
			if err != nil {
				return zero, err
			}
			d.Outputs[key] = value
		}
		d.Steps = steps
		return d, nil
	}
	d, err := expand(b.Root, path, "", "", 0, map[string]bool{})
	if err != nil {
		return zero, nil, err
	}
	if len(d.Outputs) > 0 {
		deps := []string{}
		for _, step := range d.Steps {
			deps = append(deps, step.ID)
		}
		args, _ := json.Marshal(d.Outputs)
		d.Steps = append(d.Steps, automation.Step{ID: "@outputs", Tool: "workflow_value", Args: args, DependsOn: deps, CollectOutputs: true})
	}
	if len(d.Steps) > maxExpandedSteps {
		return zero, nil, fmt.Errorf("expanded workflow exceeds %d steps", maxExpandedSteps)
	}
	d.Outputs = nil
	return d, source, nil
}

func checkBoundNames(raw json.RawMessage, names map[string]bool) error {
	_, err := recipe.MapReferences(raw, func(ref map[string]any) (any, error) {
		if v, ok := ref["$input"]; ok {
			name, ok := v.(string)
			if !ok || !names[name] {
				return nil, fmt.Errorf("child input %q has no bound value", v)
			}
		}
		return ref, nil
	})
	return err
}

func rewriteScope(raw json.RawMessage, prefix, binding string) (json.RawMessage, error) {
	return recipe.MapReferences(raw, func(ref map[string]any) (any, error) {
		if value, ok := ref["$input"]; ok {
			name, ok := value.(string)
			if !ok || len(ref) != 1 {
				return nil, fmt.Errorf("invalid input reference")
			}
			if binding == "" {
				return ref, nil
			}
			return map[string]any{"$step": binding, "pointer": "/" + strings.ReplaceAll(strings.ReplaceAll(name, "~", "~0"), "/", "~1")}, nil
		}
		name, ok := ref["$step"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid step reference")
		}
		ref["$step"] = prefix + name
		return ref, nil
	})
}
