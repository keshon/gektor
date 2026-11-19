package screen

import (
	"maps"
	"slices"

	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
	"github.com/keshon/gektor/internal/tui/components/table"
)

// PresentForm copies its fields and choices into the controls-owned draft.
func (m *Controls) PresentForm(f Form) *fieldform.Model {
	m.OpenForm(f.Title, f.Hint, f.ApplyLabel, f.Fields)
	draft := m.Dialog.Form
	draft.Destructive = f.Destructive
	draft.Choices = maps.Clone(f.Choices)
	for key, options := range draft.Choices {
		draft.Choices[key] = slices.Clone(options)
	}

	draft.ShowWhen = maps.Clone(f.ShowWhen)
	for key, conditions := range draft.ShowWhen {
		draft.ShowWhen[key] = slices.Clone(conditions)
	}

	draft.FileFields = maps.Clone(f.FileFields)
	for key, picker := range draft.FileFields {
		picker.Extensions = slices.Clone(picker.Extensions)
		draft.FileFields[key] = picker
	}
	if f.StartOnCancel {
		m.FocusForm(len(f.Fields) + 1)
	}
	return &draft.Model
}

func (m *Controls) PresentDialog(d Dialog) {
	m.Open(d.Title, d.Lines)
	m.Dialog.PreferredWidth = d.PreferredWidth
	m.Dialog.Shortcuts = slices.Clone(d.Shortcuts)
	m.Dialog.Actions = slices.Clone(d.Actions)
	if d.Table != nil {
		m.Dialog.Table = &TableState{Model: d.Table.Model, Columns: slices.Clone(d.Table.Columns), Description: d.Table.Description, Actions: slices.Clone(d.Table.Actions)}
	}
}

// RefreshTable preserves search and selection while replacing domain row snapshots.
func (m *Controls) RefreshTable(rows []table.Row) {
	snapshot := slices.Clone(rows)
	for i := range snapshot {
		snapshot[i].Cells = slices.Clone(rows[i].Cells)
	}
	m.Dialog.Table.Model.SetRows(snapshot)
}
