package workflow

import (
	"context"
	"encoding/json"
)

// Node describes one executable operation. Permission names remain stable even
// when display names or recipe versions change.
type Node struct {
	// Internal nodes are compiler-generated and omitted from authoring schemas.
	Internal       bool
	PermissionName string
	// RetrySafe declares that repeating a failed execution cannot duplicate effects.
	RetrySafe        bool
	Name             string
	Version          int
	Description      string
	InputSchema      json.RawMessage
	OutputSchema     json.RawMessage
	ReadOnly         bool
	Resources        func(json.RawMessage) ([]string, error)
	Validate         func(json.RawMessage) error
	ValidateTemplate func(json.RawMessage) error
	Execute          func(context.Context, json.RawMessage) (string, error)
}
