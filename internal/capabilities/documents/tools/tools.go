// Package tools exposes document readers to the agent.
package tools

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/documents/contract"
)

type Tool struct{ contract.Operation }

func (Tool) Mode() agent.ToolMode { return agent.Concurrent }
