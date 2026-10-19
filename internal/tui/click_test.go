package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// clickVisible clicks the first drawn occurrence of label.
//
// Tests used to work out where a button was by repeating the layout maths the
// component already does. That made them a second opinion about the renderer
// rather than a check on it: every geometry change showed up as several
// failing mouse tests and no failing renderer, and one of those renderer bugs
// had been hidden because the copied maths happened to agree with it.
func clickVisible(t *testing.T, m *model, label string) {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for y, line := range lines {
		at := strings.Index(line, label)
		if at < 0 {
			continue
		}
		x := len([]rune(line[:at])) + 1
		m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		return
	}
	t.Fatalf("no %q on screen:\n%s", label, strings.Join(lines, "\n"))
}

// clickHit clicks a hit at the position the component reports for itself.
//
// The geometry comes from the model rather than from the test, so a test
// cannot fail - or pass - because it disagreed with the renderer about where
// something was.
func clickHit(t *testing.T, m *model, index int) {
	t.Helper()
	_, hits, _, _ := m.formRows()
	x, y := m.dialog.form.Origin(m.settingsProps())
	for _, hit := range hits {
		if hit.Index != index {
			continue
		}
		m.Update(tea.MouseClickMsg{X: x + hit.X0, Y: y + hit.Y0, Button: tea.MouseLeft})
		return
	}
	t.Fatalf("no hit for index %d in %+v", index, hits)
}

// fieldHit reports the hit for a field key, so a test can name the field it
// means instead of its position in a list that a toggle can change.
func fieldHit(t *testing.T, m *model, key string) int {
	t.Helper()
	for i, f := range m.dialog.form.Fields {
		if f.Key == key {
			return i
		}
	}
	t.Fatalf("no field %q", key)
	return -1
}
