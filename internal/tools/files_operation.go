// Package tools exposes native file operations to the agent tool registry.
package tools

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/files/contract"
)

type FileOperation struct{ contract.Operation }

func (t FileOperation) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}
