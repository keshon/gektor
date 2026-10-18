package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/config"
)

// An unset temperature is not zero. It used to display as a bare 0, which
// reads as deterministic sampling, and a profile that had explicitly been
// set to the default showed the same 0 - so the field could not be trusted
// to say what a run would do.
func TestUnsetTemperatureShowsTheNumberThatWillBeUsed(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("generation")
	got := m.dialog.form.Editor.Value()
	if !strings.Contains(got, "default") {
		t.Fatalf("unset temperature shows as %q", got)
	}
	if !strings.Contains(got, "0.4") {
		t.Fatalf("unset temperature does not name the value that will be used: %q", got)
	}
}

// An explicit value shows as itself, and does not read as unset.
func TestExplicitTemperatureShowsAsItself(t *testing.T) {
	m := settingsModel(t)
	m.config.Profile.Generation.Temperature = 0.9
	m.editSettings("generation")
	got := m.dialog.form.Editor.Value()
	if strings.Contains(got, "default") || !strings.Contains(got, "0.9") {
		t.Fatalf("an explicit temperature shows as %q", got)
	}
}

// "default" is a value the field accepts, and means unset - not an error.
func TestTemperatureAcceptsDefaultAsAWord(t *testing.T) {
	got, err := config.ParseTemperature("default")
	if err != nil || got != 0 {
		t.Fatalf("ParseTemperature(default) = %v, %v", got, err)
	}
	got, err = config.ParseTemperature(" 0.7 ")
	if err != nil || got != 0.7 {
		t.Fatalf("ParseTemperature(0.7) = %v, %v", got, err)
	}
	if _, err := config.ParseTemperature("warm"); err == nil {
		t.Fatal("a nonsense temperature was accepted")
	}
}

// Removing the category rail changed where the content column starts, and the
// mouse hit-test kept the old offset: every button click landed seventeen
// cells short of its target. The buttons still drew correctly, so only a
// click proved it.
func TestSettingsButtonsAreClickableWhereTheyAreDrawn(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		m := settingsModel(t)
		m.width(tea.WindowSizeMsg{Width: width, Height: 24})
		m.editSettings("generation")
		_, hits, _, _ := m.formRows()
		if len(hits) == 0 {
			t.Fatalf("width %d: no hits", width)
		}
		cancel := hits[len(hits)-1]
		if cancel.X1 <= 0 {
			t.Fatalf("width %d: the last hit has no extent: %+v", width, cancel)
		}
		// A settings screen is anchored at the frame origin and its content
		// column starts two cells in; the button's own extent decides the
		// rest. Clicking the middle of the drawn span is the assertion.
		const frameX, contentInset, frameY = 0, 2, 2
		click := frameX + contentInset + (cancel.X0+cancel.X1)/2
		m.Update(tea.MouseClickMsg{X: click, Y: frameY + cancel.Y0, Button: tea.MouseLeft})
		if m.dialog != nil {
			t.Fatalf("width %d: clicking Cancel at its drawn position did nothing", width)
		}
	}
}

// One section per screen: no rail, and the whole width is content.
func TestSettingsModalIsNotTwoColumns(t *testing.T) {
	m := settingsModel(t)
	m.width(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.editSettings("generation")
	view := ansi.Strip(m.View().Content)
	for _, other := range []string{"Application", "Profiles", "Prompts"} {
		if strings.Contains(view, other) {
			t.Fatalf("the section modal still lists other sections (%q):\n%s", other, view)
		}
	}
	// The section it does show, and its fields.
	if !strings.Contains(view, "Generation") {
		t.Fatalf("the modal does not say which section it is:\n%s", view)
	}
	if !strings.Contains(view, "Temperature") {
		t.Fatalf("the modal does not show its field:\n%s", view)
	}
}
