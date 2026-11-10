// Package tui connects the Agent terminal host to the Workflow screen.
package tui

import (
	"context"

	"github.com/keshon/gektor/internal/tui/agent"
	"github.com/keshon/gektor/internal/tui/workflow"
)

// Config configures one terminal run.
type Config = agent.Config

// Application holds the launch configuration and persisted terminal preferences.
type Application = agent.Application

// ModeChange requests a new launch with changed application settings.
type ModeChange = agent.ModeChange

// Drafts retains unsent input across application relaunches.
type Drafts = agent.Drafts

// Run executes the task under the TUI and restores the terminal on return.
func Run(ctx context.Context, cfg Config) (string, error) {
	cfg.NewWorkflow = workflow.NewScreen
	return agent.Run(ctx, cfg)
}
