package form

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestIndependentFormsAndVisibility(t *testing.T) {
	fields := []Field{{"hidden", "Hidden", "old", false}, {"body", "Body", "one\ntwo", true}}
	a, b := New(fields, textarea.New()), New(fields, textarea.New())
	visible := func(i int) bool { return i != 0 }
	a.Focus(0, visible)
	if a.Index != len(fields)+1 {
		t.Fatal("hidden field did not preserve navigation direction")
	}
	a.Focus(1, visible)
	if a.Index != 1 || !a.Editor.Focused() {
		t.Fatal("hidden field selected")
	}
	a.Editor.SetValue("edited\nbody")
	a.Update(tea.KeyPressMsg{Code: tea.KeyTab}, visible, false, false)
	if a.Index != len(fields) || a.Values()["body"] != "edited\nbody" {
		t.Fatal("tab lost multiline edit")
	}
	if b.Fields[1].Value != "one\ntwo" || fields[1].Value != "one\ntwo" {
		t.Fatal("forms share writable field data")
	}
	intent, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, visible, false, false)
	if intent != Apply {
		t.Fatal("apply intent missing")
	}
	a.Focus(len(fields)+1, visible)
	intent, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, visible, false, false)
	if intent != Cancel {
		t.Fatal("cancel intent missing")
	}
}
func TestDestructiveFormRequiresPrimarySelection(t *testing.T) {
	m := New(nil, textarea.New())
	m.Focus(1, nil)
	intent, _ := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, nil, false, true)
	if intent != None {
		t.Fatal("save bypassed destructive confirmation")
	}
	m.Focus(0, nil)
	intent, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, false, true)
	if intent != Apply {
		t.Fatal("confirmation did not apply")
	}
}
func TestRowsAndHitGeometryUseTerminalCells(t *testing.T) {
	m := New(nil, textarea.New())
	row, hits := m.Actions(30, 4, "保存", true, Styles{})
	if lipgloss.Width(row) != 30 || len(hits) != 2 || hits[0].X1 > hits[1].X0 {
		t.Fatal("invalid button geometry")
	}
	if !strings.Contains(row, "保存") || hits[0].Y0 != 4 {
		t.Fatal("button text or row missing")
	}
}
