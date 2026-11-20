package screen

import (
	"errors"
	"testing"

	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/table"
)

func TestDialogResumeRequiresOwnedSuspendedLifetime(t *testing.T) {
	var controls, other Controls
	controls.OpenForm("edit", "", "save", []form.Field{{Key: "draft", Value: "unsaved"}})
	id := controls.DialogID()
	if controls.ResumeID(id) {
		t.Fatal("active dialog resumed")
	}
	controls.Suspend()
	controls.Open("permission", nil)
	if other.ResumeID(id) {
		t.Fatal("another host resumed the draft")
	}
	if !controls.ResumeID(id) || controls.Dialog.Form.Fields[0].Value != "unsaved" {
		t.Fatal("suspended draft was not restored")
	}
	controls.Close()
	if controls.ResumeID(id) {
		t.Fatal("closed dialog revived")
	}
	controls.Open("old", nil)
	id = controls.DialogID()
	controls.Open("replacement", nil)
	if controls.ResumeID(id) {
		t.Fatal("replaced dialog revived")
	}
}

func TestTableRefreshRejectsOldLifetimeAndCopiesSnapshots(t *testing.T) {
	var controls Controls
	view := controls.NewTable()
	controls.PresentDialog(Dialog{Title: "queue", Table: &Table{Model: view, List: true, EmptyMessage: "empty"}})
	id := controls.DialogID()
	view.Search.SetValue("message")
	rows := []table.Row{{ID: "first", Cells: []string{"message one"}}, {ID: "second", Cells: []string{"message two"}}}
	actions := []table.Action{{ID: "read", Enabled: true}}
	if !controls.UpdateTable(id, "queued", rows, actions) {
		t.Fatal("current table refresh rejected")
	}
	view.Focus = 1
	selected := controls.TableSelectedID(id)
	rows[0].Cells[0] = "caller mutation"
	actions[0].Enabled = false
	if view.Rows[0].Cells[0] != "message one" || !controls.Dialog.Table.Actions[0].Enabled {
		t.Fatal("table retained caller-owned slices")
	}
	if !controls.UpdateTable(id, "refreshed", view.Rows, controls.Dialog.Table.Actions) || view.Search.Value() != "message" || view.Focus != 1 || controls.TableSelectedID(id) != selected {
		t.Fatal("refresh lost filter, focus or selection")
	}
	controls.Open("replacement", []string{"keep"})
	if controls.UpdateTable(id, "stale", nil, nil) || controls.SetTableActions(id, nil) || controls.TableSelectedID(id) != "" || controls.Dialog.Title != "replacement" {
		t.Fatal("stale refresh touched replacement")
	}
}

func TestFormCopiesInputsAndSubmissionSnapshot(t *testing.T) {
	controls := &Controls{}
	fields := []form.Field{{Key: "name", Value: "original"}}
	choices := map[string][]string{"name": {"original", "other"}}
	files := map[string]fieldform.FilePicker{"path": {Extensions: []string{".json"}}}
	controls.PresentForm(Form{Title: "Draft", Fields: fields, Choices: choices, FileFields: files})
	dialog := controls.Dialog
	controls.TakeEvents()

	fields[0].Value = "caller changed"
	choices["name"][0] = "caller changed"
	picker := files["path"]
	picker.Extensions[0] = ".txt"
	if dialog.Form.Fields[0].Value != "original" || dialog.Form.Choices["name"][0] != "original" || dialog.Form.FileFields["path"].Extensions[0] != ".json" {
		t.Fatal("draft aliases caller inputs")
	}

	controls.ApplyForm()
	dialog.Form.Editor.SetValue("typed later")
	events := controls.TakeEvents()
	if len(events) != 1 || events[0].Kind != Apply || events[0].ID != controls.DialogID() || events[0].Values["name"] != "original" {
		t.Fatal("submission did not capture the producing draft", events)
	}
	if controls.Dialog != dialog {
		t.Fatal("controls applied or closed an unanswered submission")
	}
}

func TestReplacementIgnoresLateFormAndSaveReplies(t *testing.T) {
	controls := &Controls{}
	controls.PresentForm(Form{Title: "First"})
	firstID := controls.DialogID()
	controls.PresentForm(Form{Title: "Second"})
	second := controls.Dialog
	secondID := controls.DialogID()
	controls.CompleteFormID(firstID, errors.New("late validation"))
	controls.CompleteFormID(firstID, nil)
	controls.CompleteSaveID(firstID, nil)
	if controls.Dialog != second || second.Form.Error != "" {
		t.Fatal("late reply changed the replacement")
	}

	events := controls.TakeEvents()
	if len(events) != 3 || events[0].Kind != Opened || !events[0].First || events[1].Kind != Replaced || events[1].ID != firstID || events[2].First {
		t.Fatal("replacement broke overlay lifetime", events)
	}
	controls.Close()
	controls.Close()
	events = controls.TakeEvents()
	if len(events) != 1 || events[0].Kind != Closed || events[0].ID != secondID {
		t.Fatal("dismissal was not reported exactly once", events)
	}
}

func TestOpaqueDialogUpdatePreservesStateAndRejectsOldLifetime(t *testing.T) {
	var c Controls
	c.Props = ControlProps{Width: 80, Height: 24}
	c.Open("notes", nil)
	id := c.DialogID()
	c.Dialog.Maximized = true
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	if !c.UpdateDialog(id, "new notes", lines) {
		t.Fatal("current dialog rejected")
	}
	c.ScrollDialog(12)
	offset := c.Dialog.Offset
	if !c.UpdateDialog(id, "restyled notes", lines) || c.Dialog.Offset != offset || !c.Dialog.Maximized || c.DialogID() != id {
		t.Fatal("update replaced presentation state")
	}
	c.Open("replacement", nil)
	if c.UpdateDialog(id, "stale", lines) || c.Dialog.Title != "replacement" {
		t.Fatal("stale result changed replacement")
	}
	c.Close()
	if c.UpdateDialog(c.DialogID(), "zero", nil) {
		t.Fatal("zero identity accepted")
	}
}

func TestOpaqueFormCompletionCannotAffectReplacement(t *testing.T) {
	var c Controls
	c.OpenForm("first", "", "save", nil)
	id := c.DialogID()
	c.OpenForm("second", "", "save", nil)
	c.CompleteFormID(id, errors.New("stale error"))
	c.CompleteFormID(id, nil)
	if c.Dialog == nil || c.Dialog.Form.Error != "" {
		t.Fatal("stale completion affected replacement")
	}
	current := c.DialogID()
	c.CompleteFormID(current, errors.New("validation"))
	if c.Dialog.Form.Error != "validation" {
		t.Fatal("current validation result missing")
	}
	for _, event := range c.TakeEvents() {
		if event.ID == (DialogID{}) {
			t.Fatal("event missing identity")
		}
	}
}
