// Package workflow implements the Workflow TUI screen. New takes the screen's
// execution dependencies and draft; the caller closes the screen at shutdown.
package workflow

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workspace"
)

// Config supplies the services for one workflow screen. The screen owns Draft
// after construction; a nil draft starts an empty editor.
type Config struct {
	WS                      *workspace.Workspace
	Client                  llm.Client
	Policy                  permission.Policy
	MaxTokens, ContextLimit int
	Temperature             float64
	Draft                   *Editor
}

// New constructs an inactive screen and takes ownership of cfg.Draft.
// Call Close when the terminal launch ends to cancel its operations.
func New(ctx context.Context, cfg Config, dialogs screen.Host, presentation func() screen.Surface) (*Model, error) {
	r, err := app.WorkflowRegistry(cfg.WS, userdirs.Sub("workflows"), nil, nil)
	if err != nil {
		return nil, err
	}

	draft := cfg.Draft
	if draft == nil {
		draft = NewEditor()
	}

	ctx, cancel := context.WithCancel(ctx)

	return &Model{
		dialogs:      dialogs,
		presentation: presentation,
		config:       cfg,
		ctx:          ctx,
		shutdown:     cancel,
		editor:       draft,
		registry:     r,
		events:       make(chan tea.Msg, 16),
	}, nil
}
