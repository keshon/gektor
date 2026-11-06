package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
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
	return "Read a workflow run by run_id in this workspace's user store. Includes resolved arguments, outputs and recorded failures. Interrupted runs may have partial effects; inspection does not resume them."
}
func (Inspect) Mode() agent.ToolMode { return agent.Concurrent }
func (Inspect) ReadOnly() bool       { return true }
func (Inspect) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"run_id":{"type":"string","minLength":1,"maxLength":64}},"required":["run_id"],"additionalProperties":false}`)
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
		ID string `json:"run_id"`
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
	b, err := json.Marshal(r)
	return string(b), err
}
