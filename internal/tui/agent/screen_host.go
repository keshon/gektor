package agent

import (
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
)

func (m *Model) screenHost() screen.Host {
	return screen.Host{
		OpenForm: m.openScreenForm, OpenDialog: m.openScreenDialog,
		CloseDialog: m.closeDialog,
		FinishOperation: func() {
			if m.dialog != nil && m.dialog.onClose != nil {
				m.closeDialog()
			}
		},
	}
}

func (m *Model) openScreenDialog(d screen.Dialog) {
	m.openDialog(d.Title, d.Lines)
	m.dialog.shortcuts = d.Shortcuts
	m.dialog.save = d.Save
	m.dialog.onClose = d.OnClose
	if d.Table != nil {
		m.dialog.table = &tableDialog{
			model: d.Table.Model, columns: d.Table.Columns,
			description: d.Table.Description, actions: d.Table.Actions, activate: d.Table.Activate,
		}
	}
}

func (m *Model) openScreenForm(f screen.Form) *settings.Model {
	if f.StartOnCancel {
		m.openRemoval(f.Title, f.Hint, f.ApplyLabel, f.Fields, f.Apply)
	} else {
		m.openForm(f.Title, f.Hint, f.ApplyLabel, f.Fields, f.Apply)
	}
	m.dialog.form.Destructive = f.Destructive
	m.dialog.form.Choices = f.Choices
	m.dialog.form.FileFields = f.FileFields
	m.dialog.onClose = f.OnClose
	return &m.dialog.form.Model
}
