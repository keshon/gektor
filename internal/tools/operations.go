package tools

import (
	"github.com/keshon/gektor/internal/agent"
	doccontract "github.com/keshon/gektor/internal/capabilities/documents/contract"
	filecontract "github.com/keshon/gektor/internal/capabilities/files/contract"
	mediacontract "github.com/keshon/gektor/internal/capabilities/media/contract"
	socialcontract "github.com/keshon/gektor/internal/capabilities/social/contract"
	sshcontract "github.com/keshon/gektor/internal/capabilities/ssh/contract"
)

type DocumentOperation struct{ doccontract.Operation }

func (DocumentOperation) Mode() agent.ToolMode { return agent.Concurrent }

type FileOperation struct{ filecontract.Operation }

func (t FileOperation) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}

type MediaOperation struct{ mediacontract.Operation }

func (t MediaOperation) Mode() agent.ToolMode {
	if t.ReadOnly() {
		return agent.Concurrent
	}
	return agent.Exclusive
}

type SocialOperation struct{ socialcontract.Operation }

// Keep the existing Agent name so stored capability activations remain usable.
func (SocialOperation) Name() string         { return socialcontract.AgentName }
func (SocialOperation) Mode() agent.ToolMode { return agent.Concurrent }

type SSHOperation struct{ sshcontract.Operation }

func (SSHOperation) Mode() agent.ToolMode { return agent.Exclusive }
