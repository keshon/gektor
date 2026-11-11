// Package mcp owns MCP protocol sessions and their stdio and HTTP transports.
// Callers own authorization, credential policy, and adapting remote tools.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const timeout = 20 * time.Second
const wireMax = 10 * 1024 * 1024

type caller interface {
	Call(context.Context, string, any, any) error
}

// ToolDefinition is one remote tool advertised by tools/list.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	Method  string          `json:"method,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func discover(ctx context.Context, c caller) ([]ToolDefinition, error) {
	var defs []ToolDefinition
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result struct {
			Tools      []ToolDefinition `json:"tools"`
			NextCursor string           `json:"nextCursor"`
		}
		err := c.Call(ctx, "tools/list", params, &result)
		if err != nil {
			return nil, err
		}
		defs = append(defs, result.Tools...)
		if len(defs) > 4096 {
			return nil, fmt.Errorf("MCP tool catalog exceeds 4096 tools")
		}
		if result.NextCursor == "" {
			return defs, nil
		}
		if seen[result.NextCursor] {
			return nil, fmt.Errorf("MCP tool catalog repeated cursor")
		}
		seen[result.NextCursor] = true
		cursor = result.NextCursor
	}
	return nil, fmt.Errorf("MCP tool catalog exceeds 100 pages")
}
