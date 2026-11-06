package automation

import (
	"encoding/json"
	"time"
)

// Entry is application data, distinct from a workflow's execution Record.
type Entry struct {
	Kind         string          `json:"kind"`
	ID           string          `json:"id"`
	Revision     uint64          `json:"revision"`
	SchemaSHA256 string          `json:"schema_sha256"`
	Data         json.RawMessage `json:"data"`
	Updated      time.Time       `json:"updated"`
}
