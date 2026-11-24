package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/mcp"
	"github.com/keshon/gektor/internal/mcpmanager/credentials"
	tooloutput "github.com/keshon/gektor/internal/tools/output"
)

type mcpConnection interface {
	mcpCaller
	Close()
}

// Session applies credential redaction at the adapter boundary. The protocol
// transport owns its connection; tools own permission and output adaptation.
type Session struct {
	binding   MCPBinding
	transport mcpConnection
	secrets   []string
	// credentialCheck rejects stale credentials before transport calls.
	credentialCheck func() error
}

func (s *Session) Call(ctx context.Context, method string, params any, out any) error {
	err := ctx.Err()
	if err != nil {
		return err
	}
	if s.binding != nil {
		err = s.binding.Check()
		if err != nil {
			return err
		}
	}
	if s.credentialCheck != nil {
		err = s.credentialCheck()
		if err != nil {
			return err
		}
	}
	err = s.transport.Call(ctx, method, params, out)
	return redactCredentialError(err, s.secrets)
}

func (s *Session) Close() { s.transport.Close() }

// StartMCP starts a stdio server and adapts its catalog into Agent tools.
// The caller owns the returned session and must Close it.
func StartMCP(ctx context.Context, server, dir, command string, args ...string) ([]agent.Tool, *Session, error) {
	return startMCP(ctx, server, dir, nil, command, args...)
}

func startMCP(ctx context.Context, server, dir string, refs map[string]string, command string, args ...string) ([]agent.Tool, *Session, error) {
	refs = maps.Clone(refs)
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

	session := &Session{transport: transport, secrets: secrets}
	if len(refs) > 0 {
		session.credentialCheck = func() error {
			_, current, err := adapterEnv(refs)
			if err != nil {
				return fmt.Errorf("MCP credentials unavailable")
			}
			if !slices.Equal(current, secrets) {
				return fmt.Errorf("MCP credentials changed; reconnect")
			}
			return nil
		}
	}
	list := adaptMCPTools(server, session, defs, secrets)
	return list, session, nil
}

func adaptMCPTools(server string, session *Session, defs []mcp.ToolDefinition, secrets []string) []agent.Tool {
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

// StartBrowserMCP grants the built-in browser adapter access to its workspace artifacts.
// Configured third-party MCP servers never receive this grant.
func StartBrowserMCP(ctx context.Context, dir, command string, args ...string) ([]agent.Tool, *Session, error) {
	list, session, err := StartMCP(ctx, "browser", dir, command, args...)
	if err != nil {
		return nil, nil, err
	}
	for _, tool := range list {
		tool.(*MCPTool).browserWorkspace = dir
	}
	return list, session, nil
}

// bindHeaders snapshots the references and values before tools expose this session.
func (s *Session) bindHeaders(store credentials.Store, origin string, refs, headers map[string]string) {
	refs = maps.Clone(refs)
	headers = maps.Clone(headers)
	if len(refs) == 0 {
		return
	}
	s.credentialCheck = func() error {
		for name, ref := range refs {
			current, err := store.Resolve(ref, origin, name)
			if err != nil {
				return fmt.Errorf("MCP credentials unavailable")
			}
			if current != headers[name] {
				return fmt.Errorf("MCP credentials changed; reconnect")
			}
		}
		return nil
	}
}

// Instructions returns server guidance with configured credentials removed.
func (s *Session) Instructions() string {
	source, ok := s.transport.(interface{ Instructions() string })
	if !ok {
		return ""
	}
	return redactCredentials(source.Instructions(), s.secrets)
}
