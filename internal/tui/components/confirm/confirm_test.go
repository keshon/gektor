package confirm

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestInitialState(t *testing.T) {
	m := New("Title", "Subtitle", "yes", "no")
	if m.Focused() {
		t.Fatal("new model should not be focused")
	}
	if m.PrimaryLabel != "yes" {
		t.Fatalf("PrimaryLabel = %q, want %q", m.PrimaryLabel, "yes")
	}
	if m.SecondaryLabel != "no" {
		t.Fatalf("SecondaryLabel = %q, want %q", m.SecondaryLabel, "no")
	}
}

func TestOpenClosesFocus(t *testing.T) {
	m := New("T", "S", "a", "b")
	m.Open()
	if !m.Focused() {
		t.Fatal("Focused() should be true after Open")
	}
	m.Close()
	if m.Focused() {
		t.Fatal("Focused() should be false after Close")
	}
}

func TestUpdateReturnsPrimaryOnY(t *testing.T) {
	m := New("T", "S", "restart", "cancel")
	m.Open()
	intent, _ := m.Update(tea.KeyPressMsg{Code: 'y'})
	if intent != Primary {
		t.Fatalf("intent = %v, want Primary", intent)
	}
	if m.Focused() {
		t.Fatal("model should be closed after accepting")
	}
}

func TestUpdateReturnsPrimaryOnEnter(t *testing.T) {
	m := New("T", "S", "restart", "cancel")
	m.Open()
	intent, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if intent != Primary {
		t.Fatalf("intent = %v, want Primary", intent)
	}
}

func TestUpdateReturnsSecondaryOnN(t *testing.T) {
	m := New("T", "S", "restart", "cancel")
	m.Open()
	intent, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	if intent != Secondary {
		t.Fatalf("intent = %v, want Secondary", intent)
	}
}

func TestUpdateReturnsSecondaryOnEsc(t *testing.T) {
	m := New("T", "S", "restart", "cancel")
	m.Open()
	intent, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if intent != Secondary {
		t.Fatalf("intent = %v, want Secondary", intent)
	}
}

func TestUpdateReturnsNoneForUnrecognizedKey(t *testing.T) {
	m := New("T", "S", "restart", "cancel")
	m.Open()
	intent, _ := m.Update(tea.KeyPressMsg{Code: 'x'})
	if intent != None {
		t.Fatalf("intent = %v, want None for unrecognized key", intent)
	}
	if !m.Focused() {
		t.Fatal("model should stay open on unrecognized key")
	}
}

func TestUpdateReturnsNoneWhenNotFocused(t *testing.T) {
	m := New("T", "S", "restart", "cancel")
	// never opened
	intent, _ := m.Update(tea.KeyPressMsg{Code: 'y'})
	if intent != None {
		t.Fatalf("intent = %v, want None when not focused", intent)
	}
}

func TestViewReturnsNilWhenNotFocused(t *testing.T) {
	m := New("T", "S", "restart", "cancel")
	rows := m.View(40, Styles{})
	if rows != nil {
		t.Fatal("View should return nil when not focused")
	}
}

func TestViewContainsExpectedLines(t *testing.T) {
	m := New("Restart?", "This will restart the session.", "restart", "cancel",
		"Line 1", "Line 2")
	m.Open()
	rows := m.View(50, Styles{
		Title: lipgloss.NewStyle().Bold(true),
		Dim:   lipgloss.NewStyle(),
	})
	if len(rows) == 0 {
		t.Fatal("View returned empty rows")
	}
	text := strings.Join(rows, "\n")
	if !strings.Contains(text, "Restart?") {
		t.Fatal("View should contain title")
	}
	if !strings.Contains(text, "restart") {
		t.Fatal("View should contain primary label in buttons")
	}
	if !strings.Contains(text, "cancel") {
		t.Fatal("View should contain secondary label in buttons")
	}
	if !strings.Contains(text, "Line 1") {
		t.Fatal("View should contain body lines")
	}
}

func TestViewHintContainsKeys(t *testing.T) {
	m := New("T", "S", "go", "back")
	m.Open()
	rows := m.View(50, Styles{})
	text := strings.Join(rows, "\n")
	if !strings.Contains(text, "y go") {
		t.Fatal("hint should mention y key")
	}
	if !strings.Contains(text, "n/Esc back") {
		t.Fatal("hint should mention n/Esc keys")
	}
}

func TestBodyWithEmptyModel(t *testing.T) {
	m := New("", "", "ok", "nope")
	m.Open()
	rows := m.View(40, Styles{})
	if len(rows) == 0 {
		t.Fatal("View should still return rows for minimal model")
	}
}
