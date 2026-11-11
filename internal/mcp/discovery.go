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
