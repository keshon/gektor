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

func TestSnapshotsAndPickerOperationsRespectDialogLifetime(t *testing.T) {
	var controls, foreign Controls
	controls.PresentDialog(Dialog{Title: "Information", Lines: []string{"original"}, Actions: []DialogAction{{ID: "open", Label: "Open"}}})
	id := controls.DialogID()
	snapshot := controls.DialogSnapshot(id)
	snapshot.Lines[0] = "changed"
	snapshot.Actions[0].Label = "changed"
	if got := controls.DialogSnapshot(id); got.Lines[0] != "original" || got.Actions[0].Label != "Open" {
		t.Fatal("dialog snapshot shares controls storage")
	}
	controls.PresentForm(Form{Title: "Choice", Fields: []form.Field{{Key: "choice", Value: "one"}}, Choices: map[string][]string{"choice": {"one", "two"}}})
	current := controls.DialogID()
	if controls.OpenFormPicker(id) != nil || foreign.OpenFormPicker(current) != nil {
		t.Fatal("stale picker operation produced an effect")
	}
	if controls.DialogSnapshot(id).Kind != NoDialog || foreign.DialogSnapshot(current).Kind != NoDialog {
		t.Fatal("stale dialog returned content")
	}
	if _, ok := controls.FormSnapshot(id); ok {
		t.Fatal("stale form returned values")
	}
	if options := controls.FormOptions(id, "choice"); options != nil {
		t.Fatal("stale form returned choices")
	}
	controls.OpenFormPicker(current)
	formSnapshot, ok := controls.FormSnapshot(current)
	if !ok || !formSnapshot.PickerOpen {
		t.Fatal("current picker did not open")
	}
}
