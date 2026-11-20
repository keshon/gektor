package form

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFormHintsNameTheActionAndHideDisabledSubmission(t *testing.T) {
	f := New([]Field{{Key: "path", Label: "Path"}}, textarea.New())
	for _, disabled := range []bool{false, true} {
		rows, _ := f.Rows(Props{Title: "Open", PrimaryLabel: "Open", Width: 40, Height: 24, DisablePrimary: disabled})
		hint := ansi.Strip(rows[len(rows)-2])
		if !strings.Contains(hint, "ESC cancel") || strings.ContainsAny(hint, "·…") {
			t.Fatal("form lost its whole cancel hint", hint)
		}
		if disabled && strings.Contains(hint, "CTRL+S") || !disabled && !strings.Contains(hint, "CTRL+S open") {
			t.Fatal("submission hint does not match action availability", hint)
		}
	}
}

func TestIndependentFormsAndVisibility(t *testing.T) {
	fields := []Field{{Key: "hidden", Label: "Hidden", Value: "old"}, {Key: "body", Label: "Body", Value: "one\ntwo", Multiline: true}}
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
	intent, _ = m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, nil, false, true)
	if intent != None {
		t.Fatal("save shortcut confirmed a destructive action")
	}
	intent, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, false, true)
	if intent != Apply {
		t.Fatal("confirmation did not apply")
	}
}
func TestRowsAndHitGeometryUseTerminalCells(t *testing.T) {
	m := New(nil, textarea.New())
	row, hits := m.Actions(30, 4, "保存", true, false, Styles{})
	if lipgloss.Width(row) != 30 || len(hits) != 2 || hits[0].X1 > hits[1].X0 {
		t.Fatal("invalid button geometry")
	}
	if !strings.Contains(row, "保存") || hits[0].Y0 != 4 {
		t.Fatal("button text or row missing")
	}
}

func TestReadOnlyFieldKeepsEvidence(t *testing.T) {
	m := New([]Field{{Key: "args", Value: "original", ReadOnly: true, Multiline: true}}, textarea.New())
	m.Focus(0, nil)
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}, nil, false, false)
	m.Editor.SetValue("tampered")
	if m.Values()["args"] != "original" {
		t.Fatal("read-only evidence changed")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab}, nil, false, false)
	if m.Index != 1 {
		t.Fatal("read-only field trapped focus")
	}
}

func TestMultilineEnterEditsAndSpaceActivatesActions(t *testing.T) {
	m := New([]Field{{Key: "body", Value: "one", Multiline: true}}, textarea.New())
	m.Focus(0, nil)
	m.Editor.CursorEnd()
	intent, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, false, false)
	if intent != None || m.Index != 0 || m.Editor.Value() != "one\n" {
		t.Fatal("Enter left the multiline editor instead of inserting a newline")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab}, nil, false, false)
	intent, _ = m.Update(tea.KeyPressMsg{Code: ' '}, nil, false, false)
	if intent != Apply || m.Values()["body"] != "one\n" {
		t.Fatal("Space did not activate the focused action or editing lost the newline")
	}
}

func TestFormWrapsProseAndKeepsActionsInsideItsHeight(t *testing.T) {
	fields := []Field{{Key: "first", Label: "First"}, {Key: "second", Label: "Second"}, {Key: "third", Label: "Third"}}
	m := New(fields, textarea.New())
	hint := "Choose a connection and enter the address. The rest of this hint must wrap onto additional lines. Hint ends here."
	for _, height := range []int{18, 40} {
		props := Props{Title: "Connection", Hint: hint, Error: "The address could not be reached. Check the hostname and port before trying again.", PrimaryLabel: "Save", Width: 40, Height: height}
		rows, hits := m.Rows(props)
		if len(rows)+2 > height {
			t.Fatalf("form exceeds height %d: %d rows", height, len(rows)+2)
		}
		for _, row := range rows {
			if lipgloss.Width(row) > 34 {
				t.Fatalf("form row exceeds width: %q", row)
			}
		}
		cancel := hits[len(hits)-1]
		if cancel.Y0 >= len(rows) || !strings.Contains(ansi.Strip(rows[cancel.Y0]), "[ Cancel ]") {
			t.Fatalf("cancel button and hit disagree: %+v", cancel)
		}
		if height == 40 {
			text := ansi.Strip(strings.Join(rows, "\n"))
			if !strings.Contains(text, "Hint ends here.") || !strings.Contains(text, "trying again.") {
				t.Fatal("wrapped hint or error was cut off", text)
			}
		}
	}
}
