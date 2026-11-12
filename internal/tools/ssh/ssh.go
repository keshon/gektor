package ssh

import (
	"github.com/keshon/gektor/internal/agent"
	sshcontract "github.com/keshon/gektor/internal/capabilities/ssh/contract"
)

type Operation struct{ sshcontract.Operation }

func (Operation) Mode() agent.ToolMode { return agent.Exclusive }
