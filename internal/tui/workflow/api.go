package workflow

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
)

// State describes the retained screen without exposing its mutable editor or run.
// Reading it must not serialize the recipe or perform I/O.
type State struct {
	Active, Busy, InspectingRun  bool
	InspectorOpen, MouseCaptured bool
}

// State returns navigation flags without serializing the recipe.
func (m *Model) State() State {
	return State{
		Active:        m.active,
		Busy:          m.busy,
		InspectingRun: m.run != nil,
		InspectorOpen: m.inspectorOpen,
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

// Command handles an operator action. Effects return as Message
// values through Update.
func (m *Model) Command(action string) tea.Cmd {
	if m.ctx.Err() != nil {
		return nil
	}
	return m.command(action)
}

// Presentation supplies local controls and overlay bounds for the active screen.
func (m *Model) Presentation() screen.ControlProps {
	surface := m.presentation()
	_, height := m.canvasSize()
	props := screen.ControlProps{
		Ready:  true,
		Styles: screen.DefaultStyles(),
		Area: pane.Rect{
			X:      surface.Inset,
			Y:      surface.Area.Y + 3,
			Width:  max(1, surface.Area.Width-2*surface.Inset),
			Height: height,
		},
		Actions: m.footerActions(),
	}
	for _, group := range Menus() {
		group.Items = m.MenuItems(group.ID, true)
		for i := range group.Items {
			_, group.Items[i].Checked = m.MenuToggle(group.Items[i].Command)
			group.Items[i].Shortcut = MenuShortcut(group.Items[i].Command)
		}
		props.Menus = append(props.Menus, group)
	}
	return props
}

// CanvasSize returns the usable canvas dimensions in terminal cells.
func (m *Model) CanvasSize() (int, int) { return m.canvasSize() }

// TakeCommands transfers queued effects to the shell exactly once.
func (m *Model) TakeCommands() []tea.Cmd {
	commands := m.commands
	m.commands = nil
	return commands
}

// Message identifies asynchronous workflow results without exposing their payloads.
type Message interface{ workflowMessage() }

func (workflowAction) workflowMessage() {}

func (workflowProposal) workflowMessage() {}

func (workflowPermission) workflowMessage() {}

func (workflowResult) workflowMessage() {}

func (workflowRunID) workflowMessage() {}

func (workflowPoll) workflowMessage() {}

// Dirty reports whether the retained recipe has unsaved edits.
func (m *Model) Dirty() bool { return m.editor.Dirty() }

// Close cancels screen operations and releases its launch context.
func (m *Model) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	m.shutdown()
}

func (m *Model) Init() tea.Cmd { return nil }
