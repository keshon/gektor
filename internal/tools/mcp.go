package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/mcp"
	tooloutput "github.com/keshon/gektor/internal/tools/output"
)

type mcpConnection interface {
	mcpCaller
	Close()
}

// mcpSession applies credential redaction at the adapter boundary. The protocol
// transport owns its connection; tools own permission and output adaptation.
type mcpSession struct {
	transport mcpConnection
	secrets   []string
}

func (s *mcpSession) Call(ctx context.Context, method string, params any, out any) error {
	err := s.transport.Call(ctx, method, params, out)
	return redactCredentialError(err, s.secrets)
}

func (s *mcpSession) Close() { s.transport.Close() }

// StartMCP starts a stdio server and adapts its catalog into Agent tools.
// The caller owns the returned session and must Close it.
func StartMCP(ctx context.Context, server, dir, command string, args ...string) ([]agent.Tool, *mcpSession, error) {
	return startMCP(ctx, server, dir, nil, command, args...)
}

func startMCP(ctx context.Context, server, dir string, refs map[string]string, command string, args ...string) ([]agent.Tool, *mcpSession, error) {
	env, secrets, err := adapterEnv(refs)
	if err != nil {
		return nil, nil, err
	}
	err = validateCapabilityID(server)
	if err != nil {
		return nil, nil, err
	}

	diagnostic := &tooloutput.Buffer{}
	cfg := mcp.StdioConfig{
		Server:    server,
		Directory: dir,
		Command:   command,
		Args:      args,
		Env:       env,
		Stderr:    diagnostic,
	}
	transport, defs, err := mcp.StartStdio(ctx, cfg)
	if err != nil {
		text := strings.TrimSpace(diagnostic.String())
		if text != "" {
			err = fmt.Errorf("%w\nserver stderr: %s", err, agent.TruncateMiddle(text, 2048))
		}
		return nil, nil, redactCredentialError(err, secrets)
	}

	session := &mcpSession{transport: transport, secrets: secrets}
	list := adaptMCPTools(server, session, defs, secrets)
	return list, session, nil
}

func adaptMCPTools(server string, session *mcpSession, defs []mcp.ToolDefinition, secrets []string) []agent.Tool {
	names := assignToolNames(server, defs)
	list := make([]agent.Tool, 0, len(defs))
	for i, definition := range defs {
		definition.Description = redactCredentials(definition.Description, secrets)
		schema := redactCredentials(string(definition.InputSchema), secrets)
		definition.InputSchema = json.RawMessage(schema)
		tool := &MCPTool{
			client:   session,
			server:   server,
			def:      definition,
			toolName: names[i],
			secrets:  secrets,
		}
		list = append(list, tool)
	}
	return list
}
