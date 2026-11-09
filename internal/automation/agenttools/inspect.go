package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation/recipe"
	"github.com/keshon/gektor/internal/automation/service"
	"github.com/keshon/gektor/internal/automation/store"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
)

type Inspect struct {
	WS       *workspace.Workspace
	StoreDir string
}

func (Inspect) Name() string { return "workflow_inspect" }
func (Inspect) Description() string {
	return "Inspect a saved run. Default view=summary returns declared outputs, status, timing and failures. Use view=steps for a compact step list, view=step with step_id for arguments/output, or view=full for the complete stored record. Optional pointer selects a JSON Pointer inside declared outputs (summary) or step output (step). Values over 8 KiB are omitted with their size; narrow with pointer or request full. Inspection never resumes a run."
}
func (Inspect) Mode() agent.ToolMode { return agent.Concurrent }
func (Inspect) ReadOnly() bool       { return true }
func (Inspect) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"run_id":{"type":"string","minLength":1,"maxLength":64},"view":{"type":"string","enum":["summary","steps","step","full"]},"step_id":{"type":"string","minLength":1},"pointer":{"type":"string"}},"required":["run_id"],"additionalProperties":false}`)
}
func (t Inspect) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.WS == nil {
		return "", fmt.Errorf("workspace unavailable")
	}
	if err := agent.NewRegistry(t).ValidateCall(t.Name(), args); err != nil {
		return "", err
	}
	var in struct {
		ID      string  `json:"run_id"`
		View    string  `json:"view"`
		Step    string  `json:"step_id"`
		Pointer *string `json:"pointer"`
	}
	if err := decode(args, &in); err != nil {
		return "", err
	}
	s, err := store.OpenStore(t.StoreDir, t.WS.Root())
	if err != nil {
		return "", err
	}
	defer s.Close()
	r, err := s.Run(in.ID)
	if err != nil {
		return "", err
	}
	if in.View == "" {
		in.View = "summary"
	}
	if (in.View == "step") != (in.Step != "") {
		return "", fmt.Errorf("step_id is required only for view=step")
	}
	if in.Pointer != nil && in.View != "summary" && in.View != "step" {
		return "", fmt.Errorf("pointer requires summary or step view")
	}
	selectValue := func(raw []byte) (service.InspectionValue, error) {
		if in.Pointer == nil {
			return service.InspectValue(raw), nil
		}
		value, err := recipe.OutputAt(raw, *in.Pointer)
		if err != nil {
			return service.InspectionValue{}, err
		}
		return service.InspectValue(value), nil
	}
	var result any
	switch in.View {
	case "full":
		result = r
	case "steps":
		result = struct {
			ID     string                   `json:"run_id"`
			Status string                   `json:"status"`
			Steps  []service.StepInspection `json:"steps"`
		}{r.Record.RunID, r.Status, service.InspectSteps(r.Record)}
	case "step":
		for i, step := range r.Record.Steps {
			if step.ID != in.Step {
				continue
			}
			output, err := selectValue([]byte(step.Output))
			if err != nil {
				return "", err
			}
			result = struct {
				ID         string                  `json:"run_id"`
				Step       service.StepInspection  `json:"step"`
				Args       service.InspectionValue `json:"args"`
				Output     service.InspectionValue `json:"output"`
				Attempts   int                     `json:"attempt_count"`
				Iterations int                     `json:"iteration_count"`
			}{r.Record.RunID, service.InspectSteps(r.Record)[i], service.InspectValue(step.Args), output, len(step.Attempts), len(step.Iterations)}
			break
		}
		if result == nil {
			return "", fmt.Errorf("unknown step_id %q", in.Step)
		}
	default:
		summary := service.InspectRun(r)
		if in.Pointer != nil {
			// Select before bounding, so a small field in a large output stays accessible.
			summary.Outputs, err = selectValue(service.DeclaredOutputs(r.Record))
			if err != nil {
				return "", err
			}
		}
		result = summary
	}
	b, err := json.Marshal(result)
	return string(b), err
}
