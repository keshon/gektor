package tui

import "github.com/keshon/gektor/internal/tui/screen"

func (m *model) screenHost() screen.Host {
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

func (m *model) openScreenDialog(d screen.Dialog) {
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
