package agenttools

import (
	"context"
	"encoding/json"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/automation/nodes/record"
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
	if t.Write {
		return "record_write"
	}
	return "record_read"
}
func (t Data) Description() string {
	if t.Write {
		return "Create or update a record in this workspace's user store. Kind needs an operator-registered schema. expected_revision: 0 creates; updates require the last read revision. Conflicts fail without overwriting."
	}
	return "Read a record by kind and id from this workspace's user store, including data and its revision."
}
func (t Data) Mode() agent.ToolMode {
	if t.Write {
		return agent.Exclusive
	}
	return agent.Concurrent
}
func (t Data) ReadOnly() bool          { return !t.Write }
func (t Data) Schema() json.RawMessage { return (record.Operation{Write: t.Write}).Schema() }
func (t Data) Run(ctx context.Context, args json.RawMessage) (string, error) {
	return (record.Operation{WS: t.WS, StoreDir: t.StoreDir, Write: t.Write}).Run(ctx, args)
}

func RecordResources(args json.RawMessage) ([]string, error) {
	var in struct{ Kind, ID string }
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	return []string{in.Kind + "/" + in.ID}, nil
}
