package agent

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/mcpmanager"
	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
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
		m.returnDialogs = make(map[screen.DialogID]dialogReturn)
	}
	m.returnDialogs[m.chrome.DialogID()] = next
}

// HandleControl applies only events associated with dialogs opened by Agent.
// It runs on the application's update loop, including while Agent is hidden.
func (m *Model) HandleControl(event screen.Event) tea.Cmd {
	dialog := event.ID
	if dialog == (screen.DialogID{}) {
		return nil
	}
	switch event.Kind {
	case screen.Apply:
		if m.form.apply == nil || m.form.dialog != dialog || m.chrome.DialogID() != dialog {
			return nil
		}
		err := m.applySubmission(dialog, event.Values)
		m.chrome.CompleteFormID(dialog, err)

	case screen.DialogActionRequested:
		proposal, ok := m.lessonDialogs[dialog]
		if !ok || m.chrome.DialogID() != dialog {
			return nil
		}
		switch event.Action {
		case "save-lesson":
			memory := proposal.Memory[0]
			err := m.saveLesson(proposal, memory.When, memory.Lesson)
			m.chrome.CompleteSaveID(dialog, err)
		case "edit-lesson":
			m.editLesson(proposal)
		case "review-lesson":
			m.reviewLesson(proposal)
		}

	case screen.TableChanged:
		if m.chrome.DialogID() == dialog {
			m.PrepareControls()
		}

	case screen.TableAction:
		if m.chrome.DialogID() != dialog {
			return nil
		}
		switch {

		case m.queueDialogs[dialog] != nil:
			return m.inputQueueAction(m.queueDialogs[dialog], event.Action, event.Row)

		case dialog == m.processDialog:
			return m.processAction(m.processTable, event.Action, event.Row)

		case dialog == m.permissionDialog:
			return m.permissionAction(m.permissionTable, event.Action, event.Row)

		case dialog == m.mcpDialog:
			m.chrome.Close()
			return m.handleMCPAction(mcpActionMsg{action: event.Action, id: event.Row})
		}

	case screen.Closed, screen.Replaced, screen.Suspended:
		if m.evaluation != nil && m.evaluation.dialog == event.ID {
			m.cancelEvaluation()
		}

		next := m.returnDialogs[dialog]
		if event.Kind == screen.Suspended && (m.chrome.DialogID() == dialog || m.suspendedInputEdit != nil && m.suspendedInputEdit.dialog == dialog) {
			return nil
		}
		if m.form.dialog == dialog {
			m.form = pendingForm{}
		}
		delete(m.queueDialogs, dialog)
		delete(m.returnDialogs, dialog)
		delete(m.lessonDialogs, dialog)
		if m.processDialog == dialog {
			m.processDialog = screen.DialogID{}
		}
		if m.permissionDialog == dialog {
			m.permissionDialog = screen.DialogID{}
		}
		if m.mcpDialog == dialog {
			m.mcpDialog = screen.DialogID{}
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
	dialog := m.chrome.DialogID()
	if dialog == (screen.DialogID{}) {
		return
	}

	switch {
	case m.queueDialogs[dialog] != nil:
		m.refreshInputQueue(m.queueDialogs[dialog])
	case dialog == m.processDialog:
		m.refreshProcesses(m.processTable)
	case dialog == m.mcpDialog:
		statuses := make(map[string]mcpmanager.Status)
		for _, status := range m.mcp.List() {
			statuses[status.ID] = status
		}
		m.chrome.SetTableActions(dialog, m.mcpActions(m.mcpTable, statuses))
	}
}

func (m *Model) suspendDialog() {
	dialog := m.chrome.DialogID()
	queue := m.returnDialogs[dialog].queue
	if queue != nil && m.form.dialog == dialog && m.form.apply != nil {
		m.suspendedInputEdit = &inputQueueEdit{
			session: queue.session, stateFile: queue.stateFile, dialog: dialog, form: m.form,
		}
		m.form = pendingForm{}
	}
	m.chrome.Suspend()
}

// pendingForm belongs to the feature, while shared controls own the editable draft.
type pendingForm struct {
	dialog screen.DialogID
	apply  func(map[string]string) error
}

func (m *Model) openForm(form screen.Form, apply func(map[string]string) error) *fieldform.Model {
	draft := m.chrome.PresentForm(form)
	m.form = pendingForm{dialog: m.chrome.DialogID(), apply: apply}
	return draft
}

func (m *Model) applySubmission(dialog screen.DialogID, values map[string]string) error {
	if m.form.apply == nil || m.form.dialog != dialog || m.chrome.DialogID() != dialog {
		return nil
	}
	return m.form.apply(values)
}
