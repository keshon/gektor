package tools

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/media/contract"
)

type Tool struct{ contract.Operation }

func (t Tool) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}
