package screen

import "github.com/keshon/gektor/internal/tui/settings"

// Host presents requests using this application's shared controls.
func (m *Controls) Host() Host {
	return Host{
		OpenForm:    m.PresentForm,
		OpenDialog:  m.PresentDialog,
		CloseDialog: m.Close,
		FinishOperation: func() {
			if m.Dialog != nil && m.Dialog.OnClose != nil {
				m.Close()
			}
		},
	}
}

// PresentForm opens one requested overlay and returns its newly owned editor.
func (m *Controls) PresentForm(f Form) *settings.Model {
	if f.StartOnCancel {
		m.OpenRemoval(f.Title, f.Hint, f.ApplyLabel, f.Fields, f.Apply)
	} else {
		m.OpenForm(f.Title, f.Hint, f.ApplyLabel, f.Fields, f.Apply)
	}
	m.Dialog.Form.Destructive = f.Destructive
	m.Dialog.Form.Choices = f.Choices
	m.Dialog.Form.FileFields = f.FileFields
	m.Dialog.OnClose = f.OnClose
	return &m.Dialog.Form.Model
}

// PresentDialog opens informational content or a stable-ID table.
func (m *Controls) PresentDialog(d Dialog) {
	m.Open(d.Title, d.Lines)
	m.Dialog.Shortcuts = d.Shortcuts
	m.Dialog.Save = d.Save
	m.Dialog.OnClose = d.OnClose
	if d.Table != nil {
		m.Dialog.Table = &TableState{Model: d.Table.Model, Columns: d.Table.Columns, Description: d.Table.Description, Actions: d.Table.Actions, Activate: d.Table.Activate}
	}
}
