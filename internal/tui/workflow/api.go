package workflow

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
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

// State returns navigation flags without serializing the recipe.
func (m *Model) State() screen.WorkflowState {
	return screen.WorkflowState{
		Active: m.active, Busy: m.busy, InspectingRun: m.run != nil,
		PositionsLocked: m.positionsLocked, RawDetails: m.rawDetails, InspectorOpen: m.inspectorOpen, Overview: m.overview,
		MouseCaptured: m.dragging || m.panning || m.thumb.active,
	}
}

// Activate returns to the retained draft or inspected run without starting work.
func (m *Model) Activate() { m.active = true }

// Hide returns input ownership to Agent while retaining the draft and run.
func (m *Model) Hide() { m.active = false }

// PrepareQuit stops active work before the shell asks about an unsaved draft.
func (m *Model) PrepareQuit() bool {
	if !m.busy {
		return true
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.setNotice(pane.NoticeInfo, "Stopping workflow. Quit again once stopped.")
	return false
}

// Command handles an operator action. Effects return as screen.WorkflowMessage
// values through Update.
func (m *Model) Command(action string) tea.Cmd {
	if m.ctx.Err() != nil {
		return nil
	}
	return m.command(action)
}

// Content renders panes within the supplied surface, without application chrome.
func (m *Model) Content() string { return m.content() }

// CanvasSize returns the usable canvas dimensions in terminal cells.
func (m *Model) CanvasSize() (int, int) { return m.canvasSize() }

// Actions supplies the shortcuts for the shared application toolbar.
func (m *Model) Actions() []screen.Action { return m.footerActions() }

// TakeCommands transfers queued effects to the shell exactly once.
func (m *Model) TakeCommands() []tea.Cmd {
	commands := m.commands
	m.commands = nil
	return commands
}

func (workflowAction) WorkflowMessage()     {}
func (workflowProposal) WorkflowMessage()   {}
func (workflowPermission) WorkflowMessage() {}
func (workflowResult) WorkflowMessage()     {}
func (workflowRunID) WorkflowMessage()      {}
func (workflowPoll) WorkflowMessage()       {}

// Dirty reports whether the retained recipe has unsaved edits.
func (m *Model) Dirty() bool { return m.editor.Dirty() }
