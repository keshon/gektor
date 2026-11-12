package documents

import (
	"github.com/keshon/gektor/internal/agent"
	doccontract "github.com/keshon/gektor/internal/capabilities/documents/contract"
)

type Operation struct{ doccontract.Operation }

func (Operation) Mode() agent.ToolMode { return agent.Concurrent }
