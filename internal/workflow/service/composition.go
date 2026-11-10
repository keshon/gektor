package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
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
		if err := workflow.ValidateJSON(source, s.DefinitionSchema()); err != nil {
			return fmt.Errorf("recipe %s: %w", path, err)
		}
		var d workflow.Definition
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

// Compile validates and expands saved recipe source without executing nodes.
func (s Service) Compile(ctx context.Context, source []byte, path string) (workflow.Definition, []byte, error) {
	var zero workflow.Definition
	b, source, err := s.loadRecipeBundle(ctx, source, path)
	if err != nil {
		return zero, nil, err
	}
	// Ordinary recipes retain their exact execution definition and source digest.
	if plain, ready, err := s.plainRecipe(b); err != nil {
		return zero, nil, err
	} else if ready {
		return plain, source, nil
	}
	if err := s.authorizeRecipeBundle(ctx, b); err != nil {
		return zero, nil, err
	}
	compiler := recipeCompiler{service: s, recipes: b.Recipes, boundNames: map[string]map[string]bool{}}
	d, err := compiler.expand(b.Root, path, "", "", 0, map[string]bool{})
	if err != nil {
		return zero, nil, err
	}
	if len(d.Outputs) > 0 {
		deps := []string{}
		for _, step := range d.Steps {
			deps = append(deps, step.ID)
		}
		args, _ := json.Marshal(d.Outputs)
		d.Steps = append(d.Steps, workflow.Step{ID: "@outputs", Tool: "workflow_value", Args: args, DependsOn: deps, CollectOutputs: true})
	}
	if len(d.Steps) > maxExpandedSteps {
		return zero, nil, fmt.Errorf("expanded workflow exceeds %d steps", maxExpandedSteps)
	}
	d.Outputs = nil
	return d, source, nil
}

func (s Service) loadRecipeBundle(ctx context.Context, source []byte, path string) (recipeBundle, []byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(source, &fields); err != nil {
		return recipeBundle{}, nil, err
	}
	b := recipeBundle{}
	if fields["root"] != nil {
		if err := decode(source, &b); err != nil {
			return recipeBundle{}, nil, err
		}
	} else {
		frozen, err := s.FreezeRecipe(ctx, source, path)
		if err != nil {
			return recipeBundle{}, nil, err
		}
		if bytes.Equal(frozen, source) {
			b.Root = source
			b.Recipes = map[string]json.RawMessage{}
		} else {
			if err := decode(frozen, &b); err != nil {
				return recipeBundle{}, nil, err
			}
		}
		source = frozen
	}
	if len(source) > 64<<10 {
		return recipeBundle{}, nil, fmt.Errorf("recipe bundle exceeds 64 KiB")
	}
	return b, source, nil
}

func (s Service) plainRecipe(b recipeBundle) (workflow.Definition, bool, error) {
	if len(b.Recipes) == 0 {
		if err := workflow.ValidateJSON(b.Root, s.DefinitionSchema()); err != nil {
			return workflow.Definition{}, false, err
		}
		var plain workflow.Definition
		if err := json.Unmarshal(b.Root, &plain); err != nil {
			return workflow.Definition{}, false, err
		}
		calls := false
		for _, step := range plain.Steps {
			if strings.Contains(step.ID, "/") || strings.HasPrefix(step.ID, "@") {
				return workflow.Definition{}, false, fmt.Errorf("step ID %q uses a reserved composition namespace", step.ID)
			}
			calls = calls || step.Tool == "workflow_call"
		}
		if !calls && len(plain.Outputs) == 0 {
			return plain, true, nil
		}
	}
	return workflow.Definition{}, false, nil
}

func (s Service) authorizeRecipeBundle(ctx context.Context, b recipeBundle) error {
	for p := range b.Recipes {
		rel, err := s.WS.Relative(p)
		if err != nil || filepath.ToSlash(rel) != p {
			return fmt.Errorf("invalid bundled recipe path %q", p)
		}
		args, _ := json.Marshal(map[string]string{"path": p})
		if s.Authorize == nil {
			return fmt.Errorf("recipe read authorization unavailable")
		}
		if err := s.Authorize(ctx, "read_file", args); err != nil {
			return err
		}
	}
	return nil
}

type recipeCompiler struct {
	service    Service
	recipes    map[string]json.RawMessage
	boundNames map[string]map[string]bool
	steps      int
}

func (c *recipeCompiler) expand(raw json.RawMessage, path, prefix, binding string, depth int, stack map[string]bool) (workflow.Definition, error) {
	var zero workflow.Definition
	if depth > maxCompositionDepth || stack[path] {
		return zero, fmt.Errorf("subworkflow cycle or nesting exceeds %d at %s", maxCompositionDepth, path)
	}
	stack[path] = true
	defer delete(stack, path)
	if err := workflow.ValidateJSON(raw, c.service.DefinitionSchema()); err != nil {
		return zero, err
	}
	var d workflow.Definition
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
	steps := []workflow.Step{}
	for _, step := range d.Steps {
		var err error
		step, err = c.rewriteStep(step, prefix, binding)
		if err != nil {
			return zero, err
		}
		if step.Tool != "workflow_call" {
			steps = append(steps, step)
			c.steps++
		} else {
			expanded, err := c.expandCall(step, depth, stack)
			if err != nil {
				return zero, err
			}
			steps = append(steps, expanded...)
		}
		if c.steps > maxExpandedSteps {
			return zero, fmt.Errorf("expanded workflow exceeds %d steps", maxExpandedSteps)
		}
	}
	for key, raw := range d.Outputs {
		if binding != "" {
			if err := checkBoundNames(raw, c.boundNames[binding]); err != nil {
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

func (c *recipeCompiler) rewriteStep(step workflow.Step, prefix, binding string) (workflow.Step, error) {
	rewrite := func(raw json.RawMessage) (json.RawMessage, error) {
		if binding != "" {
			if err := checkBoundNames(raw, c.boundNames[binding]); err != nil {
				return nil, err
			}
		}
		return rewriteScope(raw, prefix, binding)
	}
	var err error
	step.Args, err = rewrite(step.Args)
	if err != nil {
		return step, err
	}
	if len(step.When) > 0 {
		step.When, err = rewrite(step.When)
		if err != nil {
			return step, err
		}
	}
	if len(step.ForEach) > 0 {
		step.ForEach, err = rewrite(step.ForEach)
		if err != nil {
			return step, err
		}
	}
	for i, dep := range step.DependsOn {
		step.DependsOn[i] = prefix + dep
	}
	if binding != "" {
		step.Gate = binding
		step.DependsOn = append(step.DependsOn, binding)
	}
	step.ID = prefix + step.ID
	return step, nil
}

func (c *recipeCompiler) expandCall(step workflow.Step, depth int, stack map[string]bool) ([]workflow.Step, error) {
	id := step.ID
	var steps []workflow.Step
	if len(step.ForEach) > 0 || step.Retry != nil {
		return nil, fmt.Errorf("workflow_call %s does not support for_each or retry", id)
	}
	if step.NodeVersion > 1 {
		return nil, fmt.Errorf("workflow_call version unavailable")
	}
	var in callInput
	if err := decode(step.Args, &in); err != nil {
		return nil, err
	}
	p, err := c.service.WS.Relative(in.Path)
	if err != nil {
		return nil, err
	}
	p = filepath.ToSlash(p)
	child, ok := c.recipes[p]
	if !ok {
		return nil, fmt.Errorf("missing bundled recipe %s", p)
	}
	var definition workflow.Definition
	if err := json.Unmarshal(child, &definition); err != nil {
		return nil, err
	}
	if len(definition.Outputs) == 0 {
		return nil, fmt.Errorf("called recipe %s must declare outputs", p)
	}
	bindID := id + "/@inputs"
	if in.Inputs == nil {
		in.Inputs = map[string]json.RawMessage{}
	}
	if definition.Inputs == nil {
		definition.Inputs = map[string]workflow.Input{}
	}
	c.boundNames[bindID] = map[string]bool{}
	for name, spec := range definition.Inputs {
		_, provided := in.Inputs[name]
		c.boundNames[bindID][name] = provided || spec.Default != nil
	}
	args, _ := json.Marshal(map[string]any{"recipe_path": p, "specs": definition.Inputs, "values": in.Inputs})
	gate := step
	gate.ID = bindID
	gate.Tool = "workflow_bind"
	gate.Args = args
	steps = append(steps, gate)
	c.steps++
	expanded, err := c.expand(child, p, id+"/", bindID, depth+1, stack)
	if err != nil {
		return nil, err
	}
	steps = append(steps, expanded.Steps...)
	deps := []string{bindID}
	for _, s := range expanded.Steps {
		deps = append(deps, s.ID)
	}
	values, _ := json.Marshal(expanded.Outputs)
	steps = append(steps, workflow.Step{ID: id, Tool: "workflow_value", Args: values, DependsOn: deps, Gate: bindID, CollectOutputs: true})
	c.steps++
	return steps, nil
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
