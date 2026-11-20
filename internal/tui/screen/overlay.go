package screen

import (
	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
	"github.com/keshon/gektor/internal/tui/components/popup"
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
	Kind  DialogKind
	Title string
	Help  bool
}

// DialogInfo reports only copied presentation metadata for the current overlay.
func (m *Controls) DialogInfo() DialogInfo {
	if m.Dialog == nil {
		return DialogInfo{}
	}
	kind := InformationDialog
	switch {
	case m.Dialog.Form != nil:
		kind = FormDialog
	case m.Dialog.Table != nil:
		kind = TableDialog
	case m.Dialog.Secret != nil:
		kind = SecretDialog
	}
	return DialogInfo{Kind: kind, Title: m.Dialog.Title, Help: m.Dialog.Help}
}

// SyncFormFocus updates focus only for the current form lifetime.
func (m *Controls) SyncFormFocus(id DialogID, active bool) bool {
	if id.owner != m || id.dialog == nil || m.Dialog != id.dialog || m.Dialog.Form == nil {
		return false
	}
	m.Dialog.Form.SyncInputFocus(active)
	return true
}

// DialogShortcuts describes the current control's input without exposing its editor.
func (m *Controls) DialogShortcuts() []popup.Shortcut {
	if m.Dialog == nil {
		return nil
	}
	if m.Dialog.Secret != nil {
		return []popup.Shortcut{{Key: "Ctrl+S", Action: "save encrypted"}, {Key: "Esc", Action: "cancel"}}
	}
	if d := m.Dialog.Table; d != nil {
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
	if f := m.Dialog.Form; f != nil {
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
