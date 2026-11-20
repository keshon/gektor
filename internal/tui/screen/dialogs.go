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
		m.Dialog.Table = &TableState{
			Model:        d.Table.Model,
			Columns:      slices.Clone(d.Table.Columns),
			Description:  d.Table.Description,
			Actions:      slices.Clone(d.Table.Actions),
			List:         d.Table.List,
			EmptyMessage: d.Table.EmptyMessage,
		}
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

// DialogID identifies one overlay lifetime without exposing its mutable state.
// The zero value identifies no dialog; IDs remain comparable after replacement.
type DialogID struct {
	dialog *DialogState
	owner  *Controls
}

// DialogID returns the identity of the currently presented overlay.
func (m *Controls) DialogID() DialogID {
	if m.Dialog == nil {
		return DialogID{}
	}
	return DialogID{dialog: m.Dialog, owner: m}
}

// UpdateDialog replaces information content in the same overlay lifetime.
// It preserves scroll and expansion and ignores replies to closed or replaced
// overlays. Forms and tables have their own draft and row update operations.
func (m *Controls) UpdateDialog(id DialogID, title string, lines []string) bool {
	if id.owner != m || id.dialog == nil || m.Dialog != id.dialog || m.Dialog.Form != nil || m.Dialog.Table != nil || m.Dialog.Secret != nil {
		return false
	}
	m.Dialog.Title = title
	m.Dialog.Lines = append([]string(nil), lines...)
	m.ScrollDialog(0)
	return true
}

// TableSelectedID returns the selected row identity, or empty for a stale dialog.
func (m *Controls) TableSelectedID(id DialogID) string {
	if id.owner != m || id.dialog == nil || m.Dialog != id.dialog || m.Dialog.Table == nil {
		return ""
	}
	return m.Dialog.Table.Model.Selected()
}

// SetTableActions copies action availability for the current table lifetime.
func (m *Controls) SetTableActions(id DialogID, actions []table.Action) bool {
	if id.owner != m || id.dialog == nil || m.Dialog != id.dialog || m.Dialog.Table == nil {
		return false
	}
	m.Dialog.Table.Actions = slices.Clone(actions)
	return true
}

// UpdateTable replaces supplied rows and labels while retaining search, selection
// and scrolling. A reply for another dialog lifetime is ignored.
func (m *Controls) UpdateTable(id DialogID, description string, rows []table.Row, actions []table.Action) bool {
	if !m.SetTableActions(id, actions) {
		return false
	}
	m.Dialog.Table.Description = description
	m.RefreshTable(rows)
	return true
}
