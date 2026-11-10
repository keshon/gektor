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

// State is the shell's read-only view of navigation and available actions.
type State struct {
	Active, Busy, InspectingRun                          bool
	PositionsLocked, RawDetails, InspectorOpen, Overview bool
	MouseCaptured                                        bool
}

func (m *Model) State() State {
	return State{
		Active: m.active, Busy: m.busy, InspectingRun: m.run != nil,
		PositionsLocked: m.positionsLocked, RawDetails: m.rawDetails, InspectorOpen: m.inspectorOpen, Overview: m.overview,
		MouseCaptured: m.dragging || m.panning || m.thumb.active,
	}
}

// Activate returns to the retained draft or inspected run without starting work.
func (m *Model) Activate() { m.active = true }
func (m *Model) Hide()     { m.active = false }

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

// Command handles an operator action. Effects return as Messages through Update.
func (m *Model) Command(action string) tea.Cmd { return m.command(action) }
func (m *Model) Content() string               { return m.content() }
func (m *Model) CanvasSize() (int, int)        { return m.canvasSize() }
func (m *Model) Actions() []screen.Action      { return m.footerActions() }

// TakeCommands transfers queued effects to the shell exactly once.
func (m *Model) TakeCommands() []tea.Cmd {
	commands := m.commands
	m.commands = nil
	return commands
}

// Message identifies asynchronous workflow results without exposing their payloads.
type Message interface{ workflowMessage() }

func (workflowAction) workflowMessage()     {}
func (workflowProposal) workflowMessage()   {}
func (workflowPermission) workflowMessage() {}
func (workflowResult) workflowMessage()     {}
func (workflowRunID) workflowMessage()      {}
func (workflowPoll) workflowMessage()       {}

// Dirty reports whether the retained recipe has unsaved edits.
func (m *Model) Dirty() bool { return m.editor.Dirty() }
