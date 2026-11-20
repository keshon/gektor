package workflow

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// HandleControl receives shared-control results on the shell's update loop.
func (m *Model) HandleControl(event screen.Event) tea.Cmd {
	dialog := event.ID
	if dialog == (screen.DialogID{}) {
		return nil
	}
	switch event.Kind {
	case screen.Apply:
		if m.form.dialog != dialog || m.dialogs.DialogID() != dialog {
			return nil
		}
		err := m.applySubmission(dialog, event.Values)
		m.dialogs.CompleteFormID(dialog, err)

	case screen.DialogActionRequested:
		if dialog != m.proposalDialog || m.dialogs.DialogID() != dialog || event.Action != "apply-proposal" {
			return nil
		}
		err := m.applyProposal()
		m.dialogs.CompleteSaveID(dialog, err)

	case screen.TableChanged:
		m.refreshRunActions()

	case screen.TableAction:
		if dialog != m.runsDialog || m.dialogs.DialogID() != dialog {
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
			m.runsDialog = screen.DialogID{}
		}
		if dialog == m.proposalDialog {
			m.proposalDialog = screen.DialogID{}
			m.proposalBefore = ""
			m.proposalDraft = workflow.Definition{}
		}
	}
	return nil
}

func (m *Model) denyPermission(dialog screen.DialogID) {
	reply := m.permissionRequests[dialog]
	if reply != nil {
		select {
		case reply <- permission.Deny:
		default:
		}
	}
	delete(m.permissionRequests, dialog)
	if m.permissionDialog == dialog {
		m.permissionDialog = screen.DialogID{}
	}
}

func (m *Model) finishPermission() {
	if m.permissionDialog != (screen.DialogID{}) && m.dialogs.DialogID() == m.permissionDialog {
		m.dialogs.Close()
	}
	m.denyPermission(m.permissionDialog)
}

// pendingForm belongs to the feature, while shared controls own the editable draft.
type pendingForm struct {
	dialog screen.DialogID
	apply  func(map[string]string) error
}

func (m *Model) openForm(form screen.Form, apply func(map[string]string) error) {
	m.dialogs.PresentForm(form)
	m.form = pendingForm{dialog: m.dialogs.DialogID(), apply: apply}
}

func (m *Model) applySubmission(dialog screen.DialogID, values map[string]string) error {
	if dialog == (screen.DialogID{}) || m.form.dialog != dialog || m.dialogs.DialogID() != dialog {
		return nil
	}
	return m.form.apply(values)
}
