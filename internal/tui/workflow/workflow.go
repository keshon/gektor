// Package workflow implements the Workflow TUI screen. New takes the screen's
// execution dependencies and draft; the caller closes the screen at shutdown.
package workflow

import (
	"context"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

// Model owns one workflow screen and its background operations.
// All mutable screen state is confined to the Bubble Tea update loop.
type Model struct {
	permissionDialog, runsDialog, proposalDialog *screen.DialogState
	permissionRequests                           map[*screen.DialogState]chan permission.Effect
	proposalBefore                               string
	proposalDraft                                workflow.Definition

	form pendingForm

	dialogs                  *screen.Controls
	surface                  screen.Surface
	config                   Config
	ctx                      context.Context
	shutdown                 context.CancelFunc
	workers                  sync.WaitGroup // joins actions before launch resources close
	commands                 []tea.Cmd
	thumb                    scrollDrag
	active, busy, inspect    bool
	overview                 bool
	rawDetails               bool
	inspectorTab             int
	portFocus                string
	portOutput, portSelected bool
	positionsLocked          bool
	inspectorOpen, panning   bool
	connectFrom              string
	connectPort              string
	editor                   *Editor
	graph                    graph.Model
	registry                 *workflow.Registry
	notice                   string
	noticeLevel              pane.NoticeLevel
	offset                   int
	dragging                 bool
	last                     graph.Point
	cancel                   context.CancelFunc
	events                   chan tea.Msg
	run                      *workflow.Run
	runID                    string
	schedules                []store.Schedule
	scheduleStatus           []service.ScheduleInspection
	workerLeaseHeld          bool
}

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
// Call Close when the terminal launch ends to cancel and join its operations.
func New(ctx context.Context, cfg Config, dialogs *screen.Controls, surface screen.Surface) (*Model, error) {
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
		dialogs:  dialogs,
		surface:  surface,
		config:   cfg,
		ctx:      ctx,
		shutdown: cancel,
		editor:   draft,
		registry: r,
		events:   make(chan tea.Msg, 16),
	}, nil
}

func (m *Model) Init() tea.Cmd { return nil }

// Close cancels screen operations and waits for worker cleanup and persistence.
func (m *Model) Close() {
	for dialog := range m.permissionRequests {
		m.denyPermission(dialog)
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.shutdown()
	m.workers.Wait()
}

// startOperation owns one screen action from launch through result delivery.
// Start the worker now so shutdown can also join work whose waiter was never run.
func (m *Model) startOperation(work func(context.Context) tea.Msg) tea.Cmd {
	if m.ctx.Err() != nil || m.busy {
		return nil
	}

	ctx, cancel := context.WithCancel(m.ctx)
	m.busy = true
	m.cancel = cancel
	screenCtx, events := m.ctx, m.events
	m.workers.Add(1)
	go func() {
		defer m.workers.Done()
		defer cancel()

		message := work(ctx)
		select {
		case events <- message:
		case <-screenCtx.Done():
		}
	}()

	return m.wait()
}

func (m *Model) wait() tea.Cmd {
	ctx, events := m.ctx, m.events
	return func() tea.Msg {
		select {
		case msg := <-events:
			return msg
		case <-ctx.Done():
			return nil
		}
	}
}

// setNotice replaces both text and severity; a later update must not inherit
// the previous failure's presentation.
func (m *Model) setNotice(level pane.NoticeLevel, text string) {
	m.notice, m.noticeLevel = text, level
}
