package mcp

import (
	"context"
	"fmt"
)

type session interface {
	caller
	notify(context.Context, map[string]any) error
}

func initialize(ctx context.Context, c session) ([]ToolDefinition, error) {
	var result any
	err := c.Call(ctx, "initialize", initialization(), &result)
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	// Older servers may reject this notification after accepting initialize.
	_ = c.notify(ctx, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	defs, err := discover(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	return defs, nil
}

func initialization() map[string]any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "gektor", "version": "1.0"},
	}
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
