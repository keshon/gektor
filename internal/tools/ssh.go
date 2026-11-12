package tools

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/ssh/contract"
)

type SSHOperation struct{ contract.Operation }

func (SSHOperation) Mode() agent.ToolMode { return agent.Exclusive }
