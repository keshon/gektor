// Package agenttools exposes document readers to the agent.
package agenttools

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/integrations/documents/adapters"
)

type Tool struct{ adapters.Operation }

func (Tool) Mode() agent.ToolMode { return agent.Concurrent }
