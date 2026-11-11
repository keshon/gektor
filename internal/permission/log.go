package permission

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// GateRecord is one operator gate answer in the optional JSONL log.
type GateRecord struct {
	TS       string `json:"ts"`
	Front    string `json:"front"`
	Tool     string `json:"tool"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
	Note     string `json:"note,omitempty"`
}

// LogGate appends operator gate answers to path as best-effort JSONL.
// It does not observe automatic policy decisions. An empty path returns inner
// unchanged. Logging failures never replace the gate's effect or error.
// Operator denial notes are extracted from "blocked by operator: ..." errors;
// other errors are recorded as the decision's reason.
func LogGate(path, front string, inner Gate) Gate {
	if path == "" {
		return inner
	}
	return func(ctx context.Context, tool, resource string, args json.RawMessage) (Effect, error) {
		eff, err := inner(ctx, tool, resource, args)
		rec := GateRecord{
			TS:       time.Now().UTC().Format(time.RFC3339),
			Front:    front,
			Tool:     tool,
			Resource: resource,
			Effect:   eff.String(),
		}
		if err != nil {
			rec.Note = strings.TrimSpace(strings.TrimPrefix(err.Error(), "blocked by operator:"))
			if rec.Note == "" && !strings.Contains(err.Error(), "blocked by operator") {
				rec.Note = err.Error()
			}
		}
		if data, merr := json.Marshal(rec); merr == nil {
			f, oerr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if oerr == nil {
				_, _ = f.Write(append(data, '\n'))
				_ = f.Close()
			}
		}
		return eff, err
	}
}
