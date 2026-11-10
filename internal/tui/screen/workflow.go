package screen

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workspace"
)

// Workflow is the terminal host's view of the workflow screen. Implementations
// own their editor, execution, asynchronous results, and cleanup. The host owns
// shared dialogs, menus, and application navigation.
type Workflow interface {
	tea.Model
	State() WorkflowState
	Activate()
	Hide()
	Close()
	PrepareQuit() bool
	Dirty() bool
	Command(string) tea.Cmd
	Content() string
	CanvasSize() (int, int)
	Actions() []Action
	TakeCommands() []tea.Cmd
}

// WorkflowState is a read-only projection used by shared navigation controls.
// Reading it must not serialize the recipe or perform I/O.
type WorkflowState struct {
	Active, Busy, InspectingRun                          bool
	PositionsLocked, RawDetails, InspectorOpen, Overview bool
	MouseCaptured                                        bool
}

// WorkflowServices supplies execution dependencies without exposing Agent state.
type WorkflowServices struct {
	WS                      *workspace.Workspace
	Client                  llm.Client
	Policy                  permission.Policy
	MaxTokens, ContextLimit int
	Temperature             float64
}

// WorkflowFactory is supplied by the application connector. The surface function
// returns the current geometry and theme after resizes or preference changes.
type WorkflowFactory func(context.Context, WorkflowServices, Host, func() Surface) (Workflow, error)

// WorkflowMessage marks asynchronous results that belong to the workflow screen.
type WorkflowMessage interface{ WorkflowMessage() }
