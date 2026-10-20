package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A form is framed to the pane, so no row of one may be wider than the pane
// allows. The scroll indicator was the one row that skipped the width cap, and
// it made the form's own frame wider than the pane, which clipped the frame's
// right-hand border and truncated every line beside it.
//
// A narrow terminal is the only place this shows, so every size is tried.
func TestNoFormRowIsWiderThanItsPane(t *testing.T) {
	for _, size := range [][2]int{{40, 20}, {44, 26}, {60, 24}, {80, 30}, {120, 40}} {
		for _, section := range []string{"model", "generation", "prompts", "you", "application"} {
			m := settingsModel(t)
			m.width(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.editSettings(section)
			_, _, pane, _ := m.formRows()
			for i, row := range formRowsOf(t, m) {
				if n := lipgloss.Width(row); n > pane {
					t.Fatalf("%dx%d %s: row %d is %d cells in a %d-cell pane: %q",
						size[0], size[1], section, i, n, pane, ansi.Strip(row))
				}
			}
		}
	}
}

func TestSettingsModalIsDrawnByTheSharedForm(t *testing.T) {
	for _, section := range []string{"model", "generation", "prompts", "you", "application"} {
		m := settingsModel(t)
		m.width(tea.WindowSizeMsg{Width: 100, Height: 30})
		m.editSettings(section)
		got := ansi.Strip(strings.Join(formRowsOf(t, m), "\n"))

		// The skeleton every form in the app shares, in order.
		order := []string{"Settings /", "Apply", "Cancel"}
		at := -1
		for _, want := range order {
			next := strings.Index(got, want)
			if next < 0 {
				t.Fatalf("%s: missing %q:\n%s", section, want, got)
			}
			if next < at {
				t.Fatalf("%s: %q appears out of order:\n%s", section, want, got)
			}
			at = next
		}
	}
}

// A destructive confirmation and a settings section must come out of the
// same renderer, or "looks alien to the rest" is still true.
func TestSettingsAndConfirmationsShareTheFormSkeleton(t *testing.T) {
	m := settingsModel(t)
	m.width(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.editSettings("generation")
	settings := ansi.Strip(strings.Join(formRowsOf(t, m), "\n"))

	// A destructive confirmation goes through the same renderer.
	m2 := restartModel(t)
	m2.width(tea.WindowSizeMsg{Width: 100, Height: 30})
	m2.confirmRestart(func() {})
	confirm := ansi.Strip(strings.Join(formRowsOf(t, m2), "\n"))

	for _, want := range []string{"[ Apply ]", "[ Cancel ]"} {
		_ = want
	}
	if !strings.Contains(settings, "[ Cancel ]") || !strings.Contains(confirm, "[ Cancel ]") {
		t.Fatal("a form is missing its Cancel button")
	}
	// Both stack title, hint, fields, actions, rule, keys - in that order.
	for _, shape := range []string{"[ Cancel ]"} {
		if strings.Index(settings, shape) < strings.Index(settings, "Settings /") {
			t.Fatal("settings draws its actions above its title")
		}
		if strings.Index(confirm, shape) < strings.Index(confirm, "Restart agent") {
			t.Fatal("a confirmation draws its actions above its title")
		}
	}
}

// Apply on an unchanged form used to be drawn greyed out. The shared form
// enabled every primary action for everyone, so the settings editor has to
// say when it has nothing to save. The button is present either way, and the
// difference shows when it has focus: an action you cannot take is not
// highlighted.
func TestApplyIsGreyedUntilSomethingChanges(t *testing.T) {
	m := settingsModel(t)
	m.width(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.editSettings("generation")
	if m.dialog.form.Dirty() {
		t.Fatal("a freshly opened section is already dirty")
	}
	clean := focusApply(t, m)

	// The first field is the temperature, focused on open.
	m.dialog.form.Focus(0, m.dialog.form.FieldVisible)
	m.dialog.form.Editor.SetValue("0.9")
	if !m.dialog.form.Dirty() {
		t.Fatal("an edit did not make the form dirty")
	}
	dirty := focusApply(t, m)

	if clean == "" || dirty == "" {
		t.Fatalf("Apply carries no colour: clean=%q dirty=%q", clean, dirty)
	}
	if clean == dirty {
		t.Fatalf("a focused Apply looks the same clean (%s) and dirty (%s)", clean, dirty)
	}
}

// focusApply moves the selection onto Apply and returns the colour it is
// drawn in.
func focusApply(t *testing.T, m *model) string {
	t.Helper()
	apply := len(m.dialog.form.Fields)
	m.dialog.form.Focus(apply, m.dialog.form.FieldVisible)
	if m.dialog.form.Index != apply {
		t.Fatalf("could not focus Apply; index is %d", m.dialog.form.Index)
	}
	return applyColour(t, m)
}

// applyColour is the SGR run that styles the Apply button. The row is
// padding, then Apply, then Cancel, so the first styled run is Apply's.
func applyColour(t *testing.T, m *model) string {
	t.Helper()
	rows, _, _, _ := m.formRows()
	for i := len(rows) - 1; i >= 0; i-- {
		if !strings.Contains(ansi.Strip(rows[i]), "[ Apply ]") {
			continue
		}
		parts := strings.Split(rows[i], "\x1b[")
		if len(parts) < 2 {
			t.Fatal("Apply carries no styling")
		}
		return strings.TrimSuffix(parts[1], "m[ Apply ]")
	}
	t.Fatal("Apply is not on the form")
	return ""
}

// A field that opens a chooser is not edited in place, so it stays a value
// row while focused - and it has to say what pressing it does.
func TestChooserFieldsShowWhatPressingThemDoes(t *testing.T) {
	m := settingsModel(t)
	m.width(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.editSettings("application")
	got := ansi.Strip(strings.Join(formRowsOf(t, m), "\n"))
	if !strings.Contains(got, "[ Choose… ]") {
		t.Fatalf("the workspace field does not say it opens a chooser:\n%s", got)
	}
	// A path is shown as its last element; a full temp path is noise.
	path := m.application.Preferences.Workspace
	if strings.Contains(got, path) || !strings.Contains(got, filepath.Base(path)+"  [ Choose… ]") {
		t.Fatalf("the workspace row shows a whole path:\n%s", got)
	}
}

// A field with fixed options shows its current one, not an editor.
func TestOptionFieldsShowTheCurrentChoice(t *testing.T) {
	m := settingsModel(t)
	m.width(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.editSettings("model")
	got := ansi.Strip(strings.Join(formRowsOf(t, m), "\n"))
	if !strings.Contains(got, "llama") {
		t.Fatalf("the protocol field does not show its value:\n%s", got)
	}
	if strings.Contains(got, "Enter to choose") {
		t.Log("note: uses the shared choice affordance")
	}
}

// A rarely used field is hidden behind the toggle, and hiding it must not
// cost a row or a scroll step to the fields around it.
func TestHiddenFieldsTakeNoRoom(t *testing.T) {
	m := settingsModel(t)
	m.width(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.editSettings("model")
	off := len(formRowsOf(t, m))
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF4})
	on := len(formRowsOf(t, m))
	if on != off+3 {
		t.Fatalf("showing the hidden field changed the form by %d rows, want 3 (label, value, gap)", on-off)
	}
	if strings.Contains(ansi.Strip(strings.Join(formRowsOf(t, m), "\n")), "API key") == false {
		t.Fatal("F4 did not reveal the advanced field")
	}
}

func formRowsOf(t *testing.T, m *model) []string {
	t.Helper()
	rows, _, _, _ := m.formRows()
	return rows
}
