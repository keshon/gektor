// Package tools exposes the public-post reader to Agent.
package tools

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/social/contract"
)

type SocialOperation struct{ contract.Operation }

// Keep the existing Agent name so stored capability activations remain usable.
func (SocialOperation) Name() string         { return contract.AgentName }
func (SocialOperation) Mode() agent.ToolMode { return agent.Concurrent }
