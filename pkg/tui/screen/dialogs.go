package screen

import (
	"maps"
	"slices"

	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/components/table"
)

// PresentForm copies its fields and choices into the controls-owned draft.
func (m *Controls) PresentForm(f Form) {
	m.OpenForm(f.Title, f.Hint, f.ApplyLabel, f.Fields)
	draft := m.dialog.Form
	draft.Title = f.Title
	if f.DialogTitle != "" {
		m.dialog.Title = f.DialogTitle
	}
	draft.SaveCopy = f.SaveCopy
	draft.Destructive = f.Destructive
	draft.Choices = maps.Clone(f.Choices)
	for key, options := range draft.Choices {
		draft.Choices[key] = slices.Clone(options)
	}
	draft.Labels = maps.Clone(f.Labels)
	for key, labels := range draft.Labels {
		draft.Labels[key] = maps.Clone(labels)
	}
	draft.InlineChoices = maps.Clone(f.InlineChoices)
	draft.NoSearchFields = maps.Clone(f.NoSearchFields)

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
}

// FormSnapshot returns edited values and feedback only for the current form.
func (m *Controls) FormSnapshot(id DialogID) (fieldform.Snapshot, bool) {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form == nil {
		return fieldform.Snapshot{}, false
	}
	return m.dialog.Form.Snapshot(), true
}

// ConfigureForm copies domain presentation without replacing the editable draft.
func (m *Controls) ConfigureForm(id DialogID, p fieldform.Presentation) bool {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form == nil {
		return false
	}
	m.dialog.Form.Configure(p)
	return true
}

// FormFeedback updates validation feedback without completing the form.
func (m *Controls) FormFeedback(id DialogID, message string) bool {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form == nil {
		return false
	}
	m.dialog.Form.Error = message
	return true
}

// FormDetailRequest captures the current file preview once for owner execution.
func (m *Controls) FormDetailRequest(id DialogID) (fieldform.DetailRequest, bool) {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form == nil {
		return fieldform.DetailRequest{}, false
	}
	return m.dialog.Form.TakeDetailRequest()
}

// AcceptFormDetail lets the controls reject results for replaced picker requests.
func (m *Controls) AcceptFormDetail(msg fieldform.DetailMsg) {
	if m.dialog != nil && m.dialog.Form != nil {
		m.dialog.Form.AcceptDetail(msg)
	}
}

// AcceptFormScan lets the controls reject results for replaced picker requests.
func (m *Controls) AcceptFormScan(msg fieldform.ScanMsg) {
	if m.dialog != nil && m.dialog.Form != nil {
		m.dialog.Form.AcceptScan(msg)
	}
}

func (m *Controls) PresentDialog(d Dialog) {
	m.Open(d.Title, d.Lines)
	m.dialog.Help = d.Help
	m.dialog.PreferredWidth = d.PreferredWidth
	m.dialog.Shortcuts = slices.Clone(d.Shortcuts)
	m.dialog.Actions = slices.Clone(d.Actions)
	if d.Table != nil {
		state := d.Table.State
		if state.owner != m || state.model == nil {
			state = m.NewTable()
		}
		if d.Table.Rows != nil {
			state.model.SetRows(copyTableRows(d.Table.Rows))
		}
		if d.Table.Selected != "" {
			state.model.Select(d.Table.Selected)
		}
		m.dialog.Table = &tableState{
			model:        state.model,
			Columns:      slices.Clone(d.Table.Columns),
			Description:  d.Table.Description,
			Actions:      slices.Clone(d.Table.Actions),
			List:         d.Table.List,
			EmptyMessage: d.Table.EmptyMessage,
		}
	}
}

func (m *Controls) refreshTable(rows []table.Row) {
	m.dialog.Table.model.SetRows(copyTableRows(rows))
}

func copyTableRows(rows []table.Row) []table.Row {
	snapshot := slices.Clone(rows)
	for i := range snapshot {
		snapshot[i].Cells = slices.Clone(rows[i].Cells)
	}
	return snapshot
}

// TableID is reusable navigation state owned by one Controls instance. Its zero
// value requests fresh state when presenting a table; hosts never edit widgets.
type TableID struct {
	owner *Controls
	model *table.Model
}

// TableSelection reads a retained table's selected row, including while hidden.
// A zero or foreign handle has no selected row.
func (m *Controls) TableSelection(id TableID) string {
	if id.owner != m || id.model == nil {
		return ""
	}
	return id.model.Selected()
}

// TableSnapshot is a copied read of the current table, including rendered hit
// geometry. Mutating it cannot change navigation or the presented rows.
type TableSnapshot struct {
	Rows             []table.Row
	Selected, Filter string
	Focus, Offset    int
	MoreOpen         bool
	Layout           table.Layout
	Actions          []table.Action
	Description      string
}

// TableSnapshot returns a detached view, or its zero value for a stale dialog.
func (m *Controls) TableSnapshot(id DialogID) TableSnapshot {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Table == nil {
		return TableSnapshot{}
	}
	model := m.dialog.Table.model
	layout := model.Layout(m.TableProps())
	layout.Actions = slices.Clone(layout.Actions)
	layout.MenuActions = slices.Clone(layout.MenuActions)
	return TableSnapshot{
		Description: m.dialog.Table.Description, Actions: slices.Clone(m.dialog.Table.Actions), Rows: copyTableRows(model.Rows), Selected: model.Selected(), Filter: model.Search.Value(),
		Focus: model.Focus, Offset: model.Offset, MoreOpen: model.MoreOpen, Layout: layout,
	}
}

// SyncTableFocus changes input focus only for the current table lifetime.
func (m *Controls) SyncTableFocus(id DialogID, active bool) bool {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Table == nil {
		return false
	}
	m.dialog.Table.model.SyncFocus(active)
	return true
}

// DialogID identifies one overlay lifetime without exposing its mutable state.
// The zero value identifies no dialog; IDs remain comparable after replacement.
type DialogID struct {
	dialog *dialogState
	owner  *Controls
}

// DialogID returns the identity of the currently presented overlay.
func (m *Controls) DialogID() DialogID {
	if m.dialog == nil {
		return DialogID{}
	}
	return DialogID{dialog: m.dialog, owner: m}
}

// UpdateDialog replaces information content in the same overlay lifetime.
// It preserves scroll and expansion and ignores replies to closed or replaced
// overlays. Forms and tables have their own draft and row update operations.
func (m *Controls) UpdateDialog(id DialogID, title string, lines []string) bool {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form != nil || m.dialog.Table != nil || m.dialog.Secret != nil {
		return false
	}
	m.dialog.Title = title
	m.dialog.Lines = append([]string(nil), lines...)
	m.ScrollDialog(0)
	return true
}

// TableSelectedID returns the selected row identity, or empty for a stale dialog.
func (m *Controls) TableSelectedID(id DialogID) string {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Table == nil {
		return ""
	}
	return m.dialog.Table.model.Selected()
}

// SetTableActions copies action availability for the current table lifetime.
func (m *Controls) SetTableActions(id DialogID, actions []table.Action) bool {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Table == nil {
		return false
	}
	m.dialog.Table.Actions = slices.Clone(actions)
	return true
}

// UpdateTable replaces supplied rows and labels while retaining search, selection
// and scrolling. A reply for another dialog lifetime is ignored.
func (m *Controls) UpdateTable(id DialogID, description string, rows []table.Row, actions []table.Action) bool {
	if !m.SetTableActions(id, actions) {
		return false
	}
	m.dialog.Table.Description = description
	m.refreshTable(rows)
	return true
}
