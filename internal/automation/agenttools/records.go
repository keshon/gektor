package agenttools

import (
	"context"
	"encoding/json"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/automation/records"
	"github.com/keshon/gektor/internal/workspace"
)

// Data reads or updates schema-validated application records in the user store.
// Schema registration is deliberately unavailable to the agent.
type Data struct {
	WS       *workspace.Workspace
	StoreDir string
	Write    bool
}

func (t Data) Name() string {
	return t.operation().Name()
}
func (t Data) Description() string {
	return t.operation().Description()
}
func (t Data) Mode() agent.ToolMode {
	if t.Write {
		return agent.Exclusive
	}
	return agent.Concurrent
}
func (t Data) ReadOnly() bool          { return t.operation().ReadOnly() }
func (t Data) Schema() json.RawMessage { return t.operation().Schema() }
func (t Data) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return t.operation().Run(ctx, args)
}

func (t Data) operation() records.Operation {
	return records.Operation{WS: t.WS, StoreDir: t.StoreDir, Write: t.Write}
}
