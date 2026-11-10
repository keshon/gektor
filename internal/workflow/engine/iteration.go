package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/recipe"
)

// Iterations are sequential and commit each outcome before the next item.
func executeIterations(ctx context.Context, s workflow.Step, h workflow.Handler, r *workflow.Run, index int, save func(workflow.Run) error) (string, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(s.ForEach, &items); err != nil || len(items) > 100 {
		return "", fmt.Errorf("invalid iteration arguments")
	}
	s.ForEach = nil
	outputs := make([]json.RawMessage, 0, len(items))
	size := 0
	for n, args := range items {
		child := s
		child.ID = fmt.Sprintf("%s[%d]", s.ID, n)
		child.Args = args
		item := workflow.StepResult{ID: child.ID, Status: "running", Args: args}
		r.Steps[index].Iterations = append(r.Steps[index].Iterations, item)
		nested := workflow.Run{Steps: []workflow.StepResult{item}}
		persist := func(v workflow.Run) error { r.Steps[index].Iterations[n] = v.Steps[0]; return save(*r) }
		if err := h.Validate(child); err != nil {
			return "", err
		}
		output, err := executeAttempts(ctx, child, h, &nested, 0, persist)
		item = nested.Steps[0]
		item.Output = output
		item.Status = "completed"
		size += len(output)
		if err == nil && (!json.Valid([]byte(output)) || size > recipe.MaxOutput-1024) {
			err = fmt.Errorf("iteration output is invalid JSON or exceeds 1 MiB")
		}
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
		}
		if len(item.Output) > recipe.MaxOutput {
			item.Output = item.Output[:recipe.MaxOutput]
		}
		r.Steps[index].Iterations[n] = item
		if e := save(*r); e != nil {
			return "", e
		}
		if err != nil {
			return "", err
		}
		outputs = append(outputs, json.RawMessage(output))
	}
	data, err := json.Marshal(map[string]any{"items": outputs})
	return string(data), err
}
