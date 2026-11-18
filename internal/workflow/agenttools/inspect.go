package agenttools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

type Inspect struct {
	WS       *workspace.Workspace
	StoreDir string
}

func (Inspect) Name() string { return "workflow_inspect" }
func (Inspect) Description() string {
	return "Inspect a saved run. Default view=summary returns declared outputs, status, timing and failures. Use view=steps for a compact step list, view=step with step_id for arguments/output, view=invocations for a paged metadata list (offset, limit 1–100), view=attempts with step_id for retry timing/failures, or view=full for the complete stored record. Optional pointer selects a JSON Pointer inside declared outputs (summary) or step output (step). Values over 8 KiB are omitted with their size; narrow with pointer or request full. Inspection never resumes a run."
}
func (Inspect) Mode() agent.ToolMode { return agent.Concurrent }
func (Inspect) ReadOnly() bool       { return true }
func (Inspect) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"run_id":{"type":"string","minLength":1,"maxLength":64},"view":{"type":"string","enum":["summary","steps","step","full","invocations","attempts"]},"step_id":{"type":"string","minLength":1},"pointer":{"type":"string"},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":100}},"required":["run_id"],"additionalProperties":false}`)
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
		Offset  int     `json:"offset"`
		Limit   int     `json:"limit"`
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
	result, err := service.InspectRecord(r, service.Inspection{
		View:    in.View,
		StepID:  in.Step,
		Pointer: in.Pointer,
		Offset:  in.Offset,
		Limit:   in.Limit,
	})
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(result)
	return string(b), err
}
