package files

import (
	"github.com/keshon/gektor/internal/agent"
	filecontract "github.com/keshon/gektor/internal/capabilities/files/contract"
)

type Operation struct{ filecontract.Operation }

func (t Operation) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}
