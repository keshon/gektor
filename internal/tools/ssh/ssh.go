package ssh

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/ssh"
	sshcontract "github.com/keshon/gektor/internal/capabilities/ssh/contract"
)

type Operation struct{ sshcontract.Operation }

func (Operation) Mode() agent.ToolMode { return agent.Exclusive }

// Connections exposes current binding metadata without resolving secrets.
type Connections struct{ Service *ssh.Service }

func (Connections) Name() string { return "ssh_connections" }
func (Connections) Description() string {
	return "List SSH names, endpoints, revisions and revocation status without connecting or resolving secrets. Use the returned name and revision with SSH tools; revoked connections cannot run."
}
func (Connections) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (Connections) Mode() agent.ToolMode { return agent.Concurrent }
func (Connections) ReadOnly() bool       { return true }
func (t Connections) Run(ctx context.Context, args json.RawMessage) (string, error) {
	err := ctx.Err()
	if err != nil {
		return "", err
	}
	err = agent.RejectUnknownFields(args)
	if err != nil {
		return "", err
	}
	if t.Service == nil {
		return "", fmt.Errorf("SSH unavailable")
	}

	connections, err := t.Service.Connections()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(struct {
		Connections []ssh.Connection `json:"connections"`
	}{Connections: connections})
	return string(data), err
}
