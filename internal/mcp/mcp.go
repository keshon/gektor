// Package mcp owns MCP protocol sessions and their stdio and HTTP transports.
// Callers own authorization, credential policy, and adapting remote tools.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
)

// ToolDefinition is one remote tool advertised by tools/list.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// StdioConfig describes an authorized server command. StartStdio copies Args and
// Env, retains Stderr, and owns the process tree until Stdio.Close completes.
// Stderr must support concurrent writes when an old transport is being replaced.
type StdioConfig struct {
	Server    string
	Directory string
	Command   string
	Args      []string
	Env       []string
	Stderr    io.Writer
}

// StartStdio launches a server and discovers its tools. The discovery context
// bounds startup only; the returned session owns its lifetime. Call Close even
// after the launch context ends. Failed startup stops and joins the process.
func StartStdio(ctx context.Context, cfg StdioConfig) (*Stdio, []ToolDefinition, error) {
	cfg.Args = slices.Clone(cfg.Args)
	cfg.Env = slices.Clone(cfg.Env)
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}

	lifetime, cancel := context.WithCancel(context.WithoutCancel(ctx))
	source := &processSource{
		config: cfg,
		ctx:    lifetime,
		cancel: cancel,
	}
	input, output, err := source.Launch(ctx)
	if err != nil {
		source.Close()
		return nil, nil, err
	}

	discovery, stop := context.WithTimeout(ctx, timeout)
	defer stop()

	client, defs, err := connect(discovery, cfg.Server, input, output)
	if err != nil {
		source.Close()
		return nil, nil, err
	}

	client.source = source

	return client, defs, nil
}

// HTTPConfig supplies an authorized endpoint and a client with the caller's
// network policy. NewHTTP copies Headers and retains Client; calls need deadlines.
type HTTPConfig struct {
	Server  string
	URL     string
	Client  *http.Client
	Headers map[string]string
}

// NewHTTP initializes a session and discovers its catalog. The caller must Close
// the returned session. Failed discovery closes any server-side session.
func NewHTTP(ctx context.Context, cfg HTTPConfig) (*HTTP, []ToolDefinition, error) {
	c := &HTTP{
		server:  cfg.Server,
		url:     cfg.URL,
		http:    cfg.Client,
		headers: maps.Clone(cfg.Headers),
	}
	if c.http == nil {
		return nil, nil, fmt.Errorf("HTTP client is required")
	}

	defs, err := initialize(ctx, c)
	if err != nil {
		c.Close()
		return nil, nil, fmt.Errorf("mcp %s: %w", cfg.Server, err)
	}

	return c, defs, nil
}
