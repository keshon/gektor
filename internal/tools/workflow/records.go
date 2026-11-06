package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/automation"
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
func (t Data) ReadOnly() bool { return !t.Write }
func (t Data) Schema() json.RawMessage {
	properties := `"kind":{"type":"string","pattern":"^[a-z0-9_-]{1,64}$"},"id":{"type":"string","pattern":"^[a-z0-9_-]{1,64}$"}`
	required := `"kind","id"`
	if t.Write {
		properties += `,"expected_revision":{"type":"integer","minimum":0,"maximum":18446744073709551614},"data":{"type":"object"}`
		required += `,"expected_revision","data"`
	}
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`)
}
func (t Data) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.WS == nil {
		return "", fmt.Errorf("workspace unavailable")
	}
	if len(args) > 64<<10 {
		return "", fmt.Errorf("record arguments exceed 64 KiB")
	}
	if err := agent.NewRegistry(t).ValidateCall(t.Name(), args); err != nil {
		return "", err
	}
	var in struct {
		Kind     string          `json:"kind"`
		ID       string          `json:"id"`
		Revision uint64          `json:"expected_revision"`
		Data     json.RawMessage `json:"data"`
	}
	if err := decode(args, &in); err != nil {
		return "", err
	}
	s, err := automation.OpenStore(t.StoreDir, t.WS.Root())
	if err != nil {
		return "", err
	}
	defer s.Close()
	var entry automation.Entry
	if t.Write {
		entry, err = s.PutEntry(ctx, in.Kind, in.ID, in.Revision, in.Data)
	} else {
		entry, err = s.Entry(in.Kind, in.ID)
	}
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(entry)
	return string(b), err
}

func RecordResources(args json.RawMessage) ([]string, error) {
	var in struct{ Kind, ID string }
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	return []string{in.Kind + "/" + in.ID}, nil
}
