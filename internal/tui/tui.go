// Package tui owns the terminal application shell and coordinates peer screens.
package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/agent"
)

// AgentConfig configures the conversation screen and its execution services.
type AgentConfig = agent.Config

// Config separates initial application navigation from Agent launch settings.
type Config struct {
	AgentConfig
	StartScreen string
}

// Application holds persisted launch settings and terminal preferences.
type Application = agent.Application

// ModeChange carries settings and retained conversation state into a new launch.
type ModeChange = agent.ModeChange

// Drafts retains unsent input across application relaunches.
type Drafts = agent.Drafts

// Run owns the terminal runtime and closes both screens before restoring input.
func Run(ctx context.Context, cfg Config) (string, error) {
	uiCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	options, restore, err := consoleInput(uiCtx)
	if err != nil {
		return "", err
	}
	defer restore()
	model, err := newModel(uiCtx, cfg)
	if err != nil {
		return "", err
	}
	defer model.Close()
	final, err := tea.NewProgram(model, append(options, tea.WithContext(uiCtx))...).Run()
	if err != nil {
		return "", err
	}
	app, ok := final.(*Model)
	if !ok {
		return "", fmt.Errorf("tui: unexpected final model")
	}
	return app.agent.Finish(app.quit)
}
