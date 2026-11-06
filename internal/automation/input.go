package automation

import "encoding/json"

// Input declares a typed workflow parameter. Values are JSON, not expressions.
type Input struct {
	Type     string          `json:"type"`
	Required bool            `json:"required,omitempty"`
	Default  json.RawMessage `json:"default,omitempty"`
}
