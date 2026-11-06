package agenttools

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/integrations/pixelita"
)

type Tool struct{ pixelita.Operation }

func (t Tool) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}
