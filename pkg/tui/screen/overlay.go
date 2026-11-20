package screen

import (
	tea "charm.land/bubbletea/v2"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/components/popup"
	"slices"
)

// DialogKind identifies the active control without exposing its mutable widget.
type DialogKind uint8

const (
	NoDialog DialogKind = iota
	InformationDialog
	FormDialog
	TableDialog
	SecretDialog
)

// DialogInfo is a value-only description of the current overlay.
type DialogInfo struct {
	Kind         DialogKind
	Title        string
	Help         bool
	Page, Offset int
	Maximized    bool
}

// DialogInfo reports only copied presentation metadata for the current overlay.
func (m *Controls) DialogInfo() DialogInfo {
	if m.dialog == nil {
		return DialogInfo{}
	}
	kind := InformationDialog
	switch {
	case m.dialog.Form != nil:
		kind = FormDialog
	case m.dialog.Table != nil:
		kind = TableDialog
	case m.dialog.Secret != nil:
		kind = SecretDialog
	}
	return DialogInfo{Kind: kind, Title: m.dialog.Title, Help: m.dialog.Help, Page: m.dialog.Page, Offset: m.dialog.Offset, Maximized: m.dialog.Maximized}
}

// SyncFormFocus updates focus only for the current form lifetime.
func (m *Controls) SyncFormFocus(id DialogID, active bool) bool {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form == nil {
		return false
	}
	m.dialog.Form.SyncInputFocus(active)
	return true
}

// DialogShortcuts describes the current control's input without exposing its editor.
func (m *Controls) DialogShortcuts() []popup.Shortcut {
	if m.dialog == nil {
		return nil
	}
	if m.dialog.Secret != nil {
		return []popup.Shortcut{{Key: "Ctrl+S", Action: "save encrypted"}, {Key: "Esc", Action: "cancel"}}
	}
	if d := m.dialog.Table; d != nil {
		if d.List {
			return []popup.Shortcut{{Key: "↑↓", Action: "select"}, {Key: "Esc", Action: "close"}}
		}
		if d.model.Layout(m.TableProps()).Capacity == 0 {
			return []popup.Shortcut{{Key: "Esc", Action: "close"}}
		}
		if d.model.MoreOpen {
			return []popup.Shortcut{{Key: "Esc", Action: "back"}}
		}
		return []popup.Shortcut{{Key: "Esc", Action: "back"}, {Key: "/", Action: "filter"}, {Key: "Tab", Action: "controls"}}
	}
	if f := m.dialog.Form; f != nil {
		if f.FeedbackOpen {
			return []popup.Shortcut{{Key: "Esc", Action: "back"}, {Key: "↑↓", Action: "scroll"}}
		}
		shortcuts := []popup.Shortcut{{Key: "Esc", Action: "cancel"}, {Key: "Tab", Action: "controls"}}
		if f.InputOwner() == fieldform.FieldInput && f.Fields[f.Index].Multiline && !f.Fields[f.Index].ReadOnly {
			shortcuts = append(shortcuts, popup.Shortcut{Key: "Ctrl+O", Action: "newline"})
		}
		return shortcuts
	}
	return []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "↑↓", Action: "scroll"}}
}

// DialogSnapshot is detached information content and presentation state.
type DialogSnapshot struct {
	DialogInfo
	Error     string
	Lines     []string
	Actions   []DialogAction
	Shortcuts []popup.Shortcut
}

// DialogSnapshot returns detached content only for the active dialog lifetime.
func (m *Controls) DialogSnapshot(id DialogID) DialogSnapshot {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog {
		return DialogSnapshot{}
	}
	snapshot := DialogSnapshot{DialogInfo: m.DialogInfo(), Lines: slices.Clone(m.dialog.Lines), Actions: slices.Clone(m.dialog.Actions), Shortcuts: slices.Clone(m.dialog.Shortcuts)}
	if m.dialog.Secret != nil {
		snapshot.Error = m.dialog.Secret.Error
	}
	return snapshot
}

// FormOrigin reports the current form's rendered origin for pointer targeting.
func (m *Controls) FormOrigin(id DialogID) (int, int) {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form == nil {
		return 0, 0
	}
	return m.dialog.Form.Origin(m.FormProps())
}

// OpenFormPicker opens choices only for the current form lifetime.
func (m *Controls) OpenFormPicker(id DialogID) tea.Cmd {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form == nil {
		return nil
	}
	return m.dialog.Form.OpenPicker(m.FormProps())
}

// FormOptions returns copied choices for the current form lifetime.
func (m *Controls) FormOptions(id DialogID, key string) []string {
	if id.owner != m || id.dialog == nil || m.dialog != id.dialog || m.dialog.Form == nil {
		return nil
	}
	return slices.Clone(m.dialog.Form.Options(key))
}
