package agent

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/mcpmanager"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/screen"
)

type dialogReturn struct {
	processes, permissions *table.Model
	queue                  *inputQueue
	mcp                    bool
	id                     string
}

func (m *Model) rememberReturn(next dialogReturn) {
	if m.returnDialogs == nil {
		m.returnDialogs = make(map[*screen.DialogState]dialogReturn)
	}
	m.returnDialogs[m.chrome.Dialog] = next
}

// HandleControl applies only events associated with dialogs opened by Agent.
// It runs on the application's update loop, including while Agent is hidden.
func (m *Model) HandleControl(event screen.Event) tea.Cmd {
	dialog := event.Dialog
	switch event.Kind {
	case screen.Apply:
		if m.formEdits[dialog] == nil || m.chrome.Dialog != dialog {
			return nil
		}
		err := m.applySubmission(dialog, event.Values)
		m.chrome.CompleteForm(dialog, err)

	case screen.Save, screen.Review:
		proposal, ok := m.lessonDialogs[dialog]
		if !ok || m.chrome.Dialog != dialog {
			return nil
		}
		if event.Kind == screen.Save {
			memory := proposal.Memory[0]
			err := m.saveLesson(proposal, memory.When, memory.Lesson)
			m.chrome.CompleteSave(dialog, err)
		} else if dialog.Save {
			m.editLesson(proposal)
		} else {
			m.reviewLesson(proposal)
		}

	case screen.TableChanged:
		m.PrepareControls()

	case screen.TableAction:
		if m.chrome.Dialog != dialog {
			return nil
		}
		view := dialog.Table.Model
		switch {

		case m.queueDialogs[dialog] != nil:
			return m.inputQueueAction(m.queueDialogs[dialog], event.Action, event.Row)

		case dialog == m.processDialog:
			return m.processAction(view, event.Action, event.Row)

		case dialog == m.permissionDialog:
			return m.permissionAction(view, event.Action, event.Row)

		case dialog == m.mcpDialog:
			m.chrome.Close()
			return m.handleMCPAction(mcpActionMsg{action: event.Action, id: event.Row})
		}

	case screen.Closed, screen.Replaced, screen.Suspended:
		if m.evaluation != nil && m.evaluation.dialog == dialog {
			m.cancelEvaluation()
		}

		next := m.returnDialogs[dialog]
		if event.Kind == screen.Suspended && (m.chrome.Dialog == dialog || m.suspendedInputEdit != nil && m.suspendedInputEdit.dialog == dialog) {
			return nil
		}
		delete(m.formEdits, dialog)
		delete(m.queueDialogs, dialog)
		delete(m.returnDialogs, dialog)
		delete(m.lessonDialogs, dialog)
		if m.processDialog == dialog {
			m.processDialog = nil
		}
		if m.permissionDialog == dialog {
			m.permissionDialog = nil
		}
		if m.mcpDialog == dialog {
			m.mcpDialog = nil
		}
		if event.Kind != screen.Closed {
			return nil
		}
		switch {

		case next.queue != nil:
			m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return inputQueueReturn{next.queue} })

		case next.processes != nil:
			m.returnToProcesses(next.processes)

		case next.permissions != nil:
			m.pendingCommands = append(m.pendingCommands, func() tea.Msg { return permissionTableMsg{next.permissions} })

		case next.mcp:
			m.queueMCP("list", next.id)
		}
	}
	return nil
}

// PrepareControls supplies table rows and actions before input or rendering.
// Reading shared presentation never refreshes Agent state or performs I/O.
func (m *Model) PrepareControls() {
	dialog := m.chrome.Dialog
	if dialog == nil || dialog.Table == nil {
		return
	}

	view := dialog.Table.Model
	switch {
	case m.queueDialogs[dialog] != nil:
		m.refreshInputQueue(m.queueDialogs[dialog])
	case dialog == m.processDialog:
		m.refreshProcesses(view)
	case dialog == m.mcpDialog:
		statuses := make(map[string]mcpmanager.Status)
		for _, status := range m.mcp.List() {
			statuses[status.ID] = status
		}
		dialog.Table.Actions = m.mcpActions(view, statuses)
	}
}

func (m *Model) suspendDialog() {
	dialog := m.chrome.Dialog
	edit, ok := m.formEdits[dialog].(editQueuedMessage)
	if ok {
		m.suspendedInputEdit = &inputQueueEdit{session: edit.q.session, stateFile: edit.q.stateFile, dialog: dialog}
	}
	m.chrome.Suspend()
}
