package social

import (
	"github.com/keshon/gektor/internal/agent"
	socialcontract "github.com/keshon/gektor/internal/capabilities/social/contract"
)

type Operation struct{ socialcontract.Operation }

// Keep the existing Agent name so stored capability activations remain usable.
func (Operation) Name() string         { return socialcontract.AgentName }
func (Operation) Mode() agent.ToolMode { return agent.Concurrent }
