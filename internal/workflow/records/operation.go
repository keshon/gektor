// Package records owns the shared application-record operation contract.
package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

// Operation reads or updates schema-validated application records in the user store.
// Schema registration is deliberately unavailable to the agent.
type Operation struct {
	WS       *workspace.Workspace
	StoreDir string
	Write    bool
}

func (t Operation) Name() string {
	if t.Write {
		return "record_write"
	}
	return "record_read"
}
func (t Operation) Description() string {
	if t.Write {
		return "Create or update a record in this workspace's user store. Kind needs an operator-registered schema. expected_revision: 0 creates; updates require the last read revision. Conflicts fail without overwriting."
	}
	return "Read a record by kind and id, including data and revision. allow_missing returns found:false, revision:0 and data:null for a missing record. The kind must still have a registered schema."
}
func (t Operation) ReadOnly() bool { return !t.Write }

// RetrySafe excludes writes because a successful commit followed by a lost
// response cannot be transparently repeated with the same revision.
func (t Operation) RetrySafe() bool { return !t.Write }
func (Operation) OutputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"kind":{"type":"string"},"id":{"type":"string"},"revision":{"type":"integer"},"data":{"type":["object","null"]},"found":{"type":"boolean"},"schema_sha256":{"type":"string"},"updated":{"type":"string"}},"required":["kind","id","revision","data"]}`)
}
func (t Operation) Schema() json.RawMessage {
	properties := `"kind":{"type":"string","pattern":"^[a-z0-9_-]{1,64}$"},"id":{"type":"string","pattern":"^[a-z0-9_-]{1,64}$"}`
	required := `"kind","id"`
	if t.Write {
		properties += `,"expected_revision":{"type":"integer","minimum":0,"maximum":18446744073709551614},"data":{"type":"object"}`
		required += `,"expected_revision","data"`
	} else {
		properties += `,"allow_missing":{"type":"boolean"}`
	}
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`)
}
func (t Operation) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.WS == nil {
		return "", fmt.Errorf("workspace unavailable")
	}
	if len(args) > 64<<10 {
		return "", fmt.Errorf("record arguments exceed 64 KiB")
	}
	if err := workflow.ValidateJSON(args, t.Schema()); err != nil {
		return "", err
	}
	var in struct {
		Kind         string          `json:"kind"`
		ID           string          `json:"id"`
		Revision     uint64          `json:"expected_revision"`
		Data         json.RawMessage `json:"data"`
		AllowMissing bool            `json:"allow_missing"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", err
	}
	s, err := store.OpenStore(t.StoreDir, t.WS.Root())
	if err != nil {
		return "", err
	}
	defer s.Close()
	var entry workflow.Entry
	if t.Write {
		entry, err = s.PutEntry(ctx, in.Kind, in.ID, in.Revision, in.Data)
	} else {
		entry, err = s.Entry(in.Kind, in.ID)
		if in.AllowMissing && errors.Is(err, store.ErrEntryNotFound) {
			b, e := json.Marshal(map[string]any{"kind": in.Kind, "id": in.ID, "found": false, "revision": 0, "data": nil})
			return string(b), e
		}
	}
	if err != nil {
		return "", err
	}
	if !t.Write && in.AllowMissing {
		b, err := json.Marshal(struct {
			workflow.Entry
			Found bool `json:"found"`
		}{entry, true})
		return string(b), err
	}
	b, err := json.Marshal(entry)
	return string(b), err
}

func Resources(args json.RawMessage) ([]string, error) {
	var in struct{ Kind, ID string }
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	return []string{in.Kind + "/" + in.ID}, nil
}
