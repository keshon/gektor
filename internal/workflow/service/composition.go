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
const maxDefinitionSteps = 128

type recipeBundle struct {
	Root    json.RawMessage            `json:"root"`
	Recipes map[string]json.RawMessage `json:"recipes"`
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
		if visitedSteps > maxDefinitionSteps {
			return fmt.Errorf("recipe closure exceeds %d steps", maxDefinitionSteps)
		}
		for _, step := range d.Steps {
			if step.Tool != "workflow_call" {
				continue
			}
			var in workflow.CallInput
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

// Compile validates saved source and attaches frozen definitions to call steps.
func (s Service) Compile(ctx context.Context, source []byte, path string) (workflow.Definition, []byte, error) {
	b, source, err := s.loadRecipeBundle(ctx, source, path)
	if err != nil {
		return workflow.Definition{}, nil, err
	}
	err = s.authorizeRecipeBundle(ctx, b)
	if err != nil {
		return workflow.Definition{}, nil, err
	}
	compiler := recipeCompiler{service: s, recipes: b.Recipes}
	d, err := compiler.compile(b.Root, path, 0, map[string]bool{})
	return d, source, err
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
	service Service
	recipes map[string]json.RawMessage
	steps   int
}

func (c *recipeCompiler) compile(raw json.RawMessage, path string, depth int, stack map[string]bool) (workflow.Definition, error) {
	var d workflow.Definition
	if depth > maxCompositionDepth || stack[path] {
		return d, fmt.Errorf("subworkflow cycle or nesting exceeds %d at %s", maxCompositionDepth, path)
	}
	stack[path] = true
	defer delete(stack, path)
	err := workflow.ValidateJSON(raw, c.service.DefinitionSchema())
	if err != nil {
		return d, err
	}
	err = json.Unmarshal(raw, &d)
	if err != nil {
		return d, err
	}
	c.steps += len(d.Steps)
	if c.steps > maxDefinitionSteps {
		return d, fmt.Errorf("recipe closure exceeds %d steps", maxDefinitionSteps)
	}

	for i := range d.Steps {
		step := &d.Steps[i]
		if strings.Contains(step.ID, "/") || strings.HasPrefix(step.ID, "@") {
			return d, fmt.Errorf("step ID %q uses a reserved composition namespace", step.ID)
		}
		if step.Tool != "workflow_call" {
			continue
		}
		if len(step.ForEach) > 0 || step.Retry != nil {
			return d, fmt.Errorf("workflow_call %s does not support for_each or retry", step.ID)
		}
		var in workflow.CallInput
		err = decode(step.Args, &in)
		if err != nil {
			return d, fmt.Errorf("workflow_call requires a literal recipe_path: %w", err)
		}
		var childPath string
		childPath, err = c.service.WS.Relative(in.Path)
		if err != nil {
			return d, err
		}
		childPath = filepath.ToSlash(childPath)
		in.Path = childPath
		step.Args, err = json.Marshal(in)
		if err != nil {
			return d, err
		}
		childSource, ok := c.recipes[childPath]
		if !ok {
			return d, fmt.Errorf("missing bundled recipe %s", childPath)
		}
		var child workflow.Definition
		child, err = c.compile(childSource, childPath, depth+1, stack)
		if err != nil {
			return d, err
		}
		if len(child.Outputs) == 0 {
			return d, fmt.Errorf("called recipe %s must declare outputs", childPath)
		}
		step.Child = &child
	}
	if len(d.Outputs) > 0 {
		deps := make([]string, 0, len(d.Steps))
		for _, step := range d.Steps {
			deps = append(deps, step.ID)
		}
		var args json.RawMessage
		args, err = json.Marshal(d.Outputs)
		if err != nil {
			return d, err
		}
		d.Steps = append(d.Steps, workflow.Step{ID: "@outputs", Tool: "workflow_value", Args: args, DependsOn: deps, CollectOutputs: true})
	}
	if depth > 0 {
		h := &handler{registry: c.service.Registry, templates: true}
		err = recipe.ValidateTemplate(d, h)
	}
	return d, err
}

func (h *handler) validateCall(s workflow.Step) error {
	if s.Child == nil {
		return fmt.Errorf("workflow_call requires a frozen child definition")
	}
	if len(s.ForEach) > 0 || s.Retry != nil {
		return fmt.Errorf("workflow_call does not support for_each or retry")
	}
	var in workflow.CallInput
	err := decode(s.Args, &in)
	if err != nil {
		return err
	}
	if !h.templates || !recipe.HasOutputReferences(s.Args) {
		_, err = recipe.BindInputs(s.Child.Inputs, in.Inputs)
		if err != nil {
			return err
		}
	}
	for name, value := range in.Inputs {
		spec, ok := s.Child.Inputs[name]
		if !ok {
			return fmt.Errorf("unknown input %q", name)
		}
		if h.templates && recipe.HasOutputReferences(value) {
			continue
		}
		err = recipe.CheckInput(value, spec.Type)
		if err != nil {
			return fmt.Errorf("input %s: %w", name, err)
		}
	}
	names := map[string]bool{}
	for name, spec := range s.Child.Inputs {
		_, provided := in.Inputs[name]
		names[name] = provided || spec.Default != nil
		if spec.Required && !names[name] {
			return fmt.Errorf("required input %q missing", name)
		}
		if spec.Default != nil {
			err = recipe.CheckInput(spec.Default, spec.Type)
			if err != nil {
				return err
			}
		}
	}
	for _, childStep := range s.Child.Steps {
		for _, raw := range []json.RawMessage{childStep.Args, childStep.ForEach, childStep.When} {
			if len(raw) == 0 {
				continue
			}
			_, err = recipe.MapReferences(raw, func(ref map[string]any) (any, error) {
				if name, ok := ref["$input"].(string); ok && !names[name] {
					return nil, fmt.Errorf("child input %q has no bound value", name)
				}
				return ref, nil
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}
