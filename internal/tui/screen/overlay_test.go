package screen

import (
	"slices"
	"testing"

	"charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

func TestDialogMetadataAndShortcutsFollowControlLifetime(t *testing.T) {
	var controls Controls
	if controls.DialogInfo().Kind != NoDialog || controls.DialogShortcuts() != nil {
		t.Fatal("closed control reports an overlay")
	}
	controls.PresentDialog(Dialog{Title: "Commands", Help: true})
	info := controls.DialogInfo()
	if info.Kind != InformationDialog || !info.Help || info.Title != "Commands" {
		t.Fatal(info)
	}
	info.Title = "caller mutation"
	if controls.DialogInfo().Title != "Commands" {
		t.Fatal("metadata exposed live state")
	}
	shortcuts := controls.DialogShortcuts()
	shortcuts[0].Action = "caller mutation"
	if controls.DialogShortcuts()[0].Action != "close" {
		t.Fatal("shortcut snapshot exposed live state")
	}
	controls.PresentForm(Form{Title: "Notes", Fields: []form.Field{{Key: "notes", Multiline: true}}})
	id := controls.DialogID()
	controls.FocusForm(0)
	if !controls.SyncFormFocus(id, true) || controls.DialogInfo().Kind != FormDialog || controls.DialogInfo().Help {
		t.Fatal("form retained help state or lost focus")
	}
	if !slices.Contains(controls.DialogShortcuts(), popup.Shortcut{Key: "Ctrl+O", Action: "newline"}) {
		t.Fatal("multiline editor lost its newline hint")
	}
	controls.FormKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if slices.Contains(controls.DialogShortcuts(), popup.Shortcut{Key: "Ctrl+O", Action: "newline"}) {
		t.Fatal("button focus advertises editor input")
	}
	controls.PresentForm(Form{Title: "Replacement", Fields: []form.Field{{Key: "value"}}})
	controls.FocusForm(0)
	var foreign Controls
	if controls.SyncFormFocus(id, false) || foreign.SyncFormFocus(controls.DialogID(), false) {
		t.Fatal("stale or foreign form changed focus")
	}
	controls.Close()
	if controls.DialogInfo().Kind != NoDialog || controls.DialogShortcuts() != nil {
		t.Fatal("closed form left stale metadata")
	}
}
