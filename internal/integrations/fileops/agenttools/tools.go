// Package agenttools exposes native file operations to the agent tool registry.
package agenttools

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/integrations/fileops/adapters"
)

type Tool struct{ adapters.Operation }

func (t Tool) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}
