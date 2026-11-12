package workflow

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/workflow"
)

// HandleControl receives shared-control results on the shell's update loop.
func (m *Model) HandleControl(event screen.Event) tea.Cmd {
	dialog := event.Dialog
	switch event.Kind {
	case screen.Apply:
		if m.form.dialog != dialog || m.dialogs.Dialog != dialog {
			return nil
		}
		err := m.applySubmission(dialog, event.Values)
		m.dialogs.CompleteForm(dialog, err)

	case screen.Save:
		if dialog != m.proposalDialog || m.dialogs.Dialog != dialog {
			return nil
		}
		err := m.applyProposal()
		m.dialogs.CompleteSave(dialog, err)

	case screen.TableChanged:
		m.refreshRunActions()

	case screen.TableAction:
		if dialog != m.runsDialog || m.dialogs.Dialog != dialog {
			return nil
		}
		if event.Action == "details" && event.Row != "" && m.ctx.Err() == nil && !m.busy {
			m.dialogs.Close()
			m.queueAction("inspect-run", event.Row)
		}

	case screen.Closed, screen.Replaced, screen.Suspended:
		if m.form.dialog == dialog {
			m.form = pendingForm{}
		}
		m.denyPermission(dialog)
		if m.runsDialog == dialog {
			m.runsDialog = nil
		}
		if dialog == m.proposalDialog {
			m.proposalDialog = nil
			m.proposalBefore = ""
			m.proposalDraft = workflow.Definition{}
		}
	}
	return nil
}

func (m *Model) denyPermission(dialog *screen.DialogState) {
	reply := m.permissionRequests[dialog]
	if reply != nil {
		select {
		case reply <- permission.Deny:
		default:
		}
	}
	delete(m.permissionRequests, dialog)
	if m.permissionDialog == dialog {
		m.permissionDialog = nil
	}
}

func (m *Model) finishPermission() {
	if m.permissionDialog != nil && m.dialogs.Dialog == m.permissionDialog {
		m.dialogs.Close()
	}
	m.denyPermission(m.permissionDialog)
}

// pendingForm belongs to the feature, while shared controls own the editable draft.
type pendingForm struct {
	dialog *screen.DialogState
	apply  func(map[string]string) error
}

func (m *Model) openForm(form screen.Form, apply func(map[string]string) error) *settings.Model {
	draft := m.dialogs.PresentForm(form)
	m.form = pendingForm{dialog: m.dialogs.Dialog, apply: apply}
	return draft
}

func (m *Model) applySubmission(dialog *screen.DialogState, values map[string]string) error {
	if m.form.dialog != dialog || m.dialogs.Dialog != dialog {
		return nil
	}
	return m.form.apply(values)
}
