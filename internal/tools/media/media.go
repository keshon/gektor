package media

import (
	"github.com/keshon/gektor/internal/agent"
	mediacontract "github.com/keshon/gektor/internal/capabilities/media/contract"
)

type Operation struct{ mediacontract.Operation }

func (t Operation) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}
