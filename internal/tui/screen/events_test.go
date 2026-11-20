package screen

import (
	"charm.land/bubbletea/v2"
	"errors"
	"fmt"
	"github.com/keshon/gektor/internal/tui/components/pane"
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
	if !controls.ResumeID(id) || controls.dialog.Form.Fields[0].Value != "unsaved" {
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
	state := controls.NewTable()
	view := state.model
	controls.PresentDialog(Dialog{Title: "queue", Table: &Table{State: state, List: true, EmptyMessage: "empty"}})
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
	if view.Rows[0].Cells[0] != "message one" || !controls.dialog.Table.Actions[0].Enabled {
		t.Fatal("table retained caller-owned slices")
	}
	if !controls.UpdateTable(id, "refreshed", view.Rows, controls.dialog.Table.Actions) || view.Search.Value() != "message" || view.Focus != 1 || controls.TableSelectedID(id) != selected {
		t.Fatal("refresh lost filter, focus or selection")
	}
	controls.Open("replacement", []string{"keep"})
	if controls.UpdateTable(id, "stale", nil, nil) || controls.SetTableActions(id, nil) || controls.TableSelectedID(id) != "" || controls.dialog.Title != "replacement" {
		t.Fatal("stale refresh touched replacement")
	}
}

func TestFormCopiesInputsAndSubmissionSnapshot(t *testing.T) {
	controls := &Controls{}
	fields := []form.Field{{Key: "name", Value: "original"}}
	choices := map[string][]string{"name": {"original", "other"}}
	files := map[string]fieldform.FilePicker{"path": {Extensions: []string{".json"}}}
	controls.PresentForm(Form{Title: "Draft", Fields: fields, Choices: choices, FileFields: files})
	dialog := controls.dialog
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
	if controls.dialog != dialog {
		t.Fatal("controls applied or closed an unanswered submission")
	}
}

func TestReplacementIgnoresLateFormAndSaveReplies(t *testing.T) {
	controls := &Controls{}
	controls.PresentForm(Form{Title: "First"})
	firstID := controls.DialogID()
	controls.PresentForm(Form{Title: "Second"})
	second := controls.dialog
	secondID := controls.DialogID()
	controls.CompleteFormID(firstID, errors.New("late validation"))
	controls.CompleteFormID(firstID, nil)
	controls.CompleteSaveID(firstID, nil)
	if controls.dialog != second || second.Form.Error != "" {
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
	c.dialog.Maximized = true
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	if !c.UpdateDialog(id, "new notes", lines) {
		t.Fatal("current dialog rejected")
	}
	c.ScrollDialog(12)
	offset := c.dialog.Offset
	if !c.UpdateDialog(id, "restyled notes", lines) || c.dialog.Offset != offset || !c.dialog.Maximized || c.DialogID() != id {
		t.Fatal("update replaced presentation state")
	}
	c.Open("replacement", nil)
	if c.UpdateDialog(id, "stale", lines) || c.dialog.Title != "replacement" {
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
	if c.dialog == nil || c.dialog.Form.Error != "" {
		t.Fatal("stale completion affected replacement")
	}
	current := c.DialogID()
	c.CompleteFormID(current, errors.New("validation"))
	if c.dialog.Form.Error != "validation" {
		t.Fatal("current validation result missing")
	}
	for _, event := range c.TakeEvents() {
		if event.ID == (DialogID{}) {
			t.Fatal("event missing identity")
		}
	}
}

func TestRetainedTableNavigationAndSnapshotIsolation(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		for _, size := range [][2]int{{40, 20}, {80, 24}} {
			t.Run(fmt.Sprintf("%s/%dx%d", theme, size[0], size[1]), func(t *testing.T) {
				palette := NewPalette(theme)
				controls := &Controls{Props: ControlProps{Width: size[0], Height: size[1], Area: pane.Rect{Width: size[0], Height: size[1]}, Palette: palette, Styles: palette.Styles()}}
				state := controls.NewTable()
				rows := make([]table.Row, 60)
				for i := range rows {
					rows[i] = table.Row{ID: fmt.Sprint(i), Cells: []string{fmt.Sprintf("record %02d", i)}}
				}
				columns := []table.Column{{Label: "Record"}}
				actions := []table.Action{{ID: "details", Label: "Open", Key: "enter", Enabled: true}}
				controls.PresentDialog(Dialog{Title: "Records", Table: &Table{State: state, Rows: rows, Columns: columns, Actions: actions}})
				stale := controls.DialogID()
				// Requests transfer snapshots, including the nested cell slices.
				rows[0].Cells[0] = "caller changed"
				columns[0].Label = "caller changed"
				actions[0].Enabled = false
				if got := controls.TableSnapshot(stale); got.Rows[0].Cells[0] != "record 00" || !got.Layout.Actions[0].Enabled {
					t.Fatal("presentation retained request-owned data")
				}
				controls.DialogInput(tea.KeyPressMsg{Code: '/'})
				controls.DialogInput(tea.PasteMsg{Content: "record"})
				controls.DialogInput(tea.KeyPressMsg{Code: tea.KeyEsc})
				controls.DialogInput(tea.KeyPressMsg{Code: tea.KeyPgDown})
				controls.DialogInput(tea.KeyPressMsg{Code: tea.KeyPgDown})
				controls.DialogInput(tea.KeyPressMsg{Code: tea.KeyTab})
				before := controls.TableSnapshot(stale)
				if before.Offset == 0 || before.Focus != 1 || before.Filter != "record" {
					t.Fatal("test did not establish scrolled, filtered navigation", before)
				}
				controls.Open("Details", []string{"Read a record"})
				if controls.TableSelection(state) != before.Selected {
					t.Fatal("hidden table lost selection")
				}
				controls.PresentDialog(Dialog{Title: "Records again", Table: &Table{State: state, Columns: []table.Column{{Label: "Record"}}, Actions: []table.Action{{ID: "details", Enabled: true}}}})
				current := controls.DialogID()
				after := controls.TableSnapshot(current)
				if after.Selected != before.Selected || after.Filter != before.Filter || after.Focus != before.Focus || after.Offset != before.Offset {
					t.Fatal("overlay return reset navigation", before, after)
				}
				if controls.UpdateTable(stale, "late reply", nil, nil) || controls.SetTableActions(stale, nil) || controls.SyncTableFocus(stale, false) {
					t.Fatal("old overlay lifetime changed the reused table")
				}
				after.Rows[0].Cells[0] = "snapshot mutation"
				after.Layout.Actions[0].Enabled = false
				if got := controls.TableSnapshot(current); got.Rows[0].Cells[0] != "record 00" || !got.Layout.Actions[0].Enabled {
					t.Fatal("read snapshot exposed mutable table state")
				}
				// Retaining state does not prevent an explicit empty snapshot from clearing it.
				controls.PresentDialog(Dialog{Title: "Empty", Table: &Table{State: state, Rows: []table.Row{}}})
				if controls.TableSelection(state) != "" || len(controls.TableSnapshot(controls.DialogID()).Rows) != 0 {
					t.Fatal("explicit empty rows retained obsolete records")
				}
			})
		}
	}
}

func TestTableHandlesBelongToTheirControls(t *testing.T) {
	var first, second Controls
	state := first.NewTable()
	rows := []table.Row{{ID: "one", Cells: []string{"First"}}, {ID: "two", Cells: []string{"Second"}}}
	first.PresentDialog(Dialog{Title: "First", Table: &Table{State: state, Rows: rows, Selected: "two"}})
	firstDialog := first.DialogID()
	second.PresentDialog(Dialog{Title: "Second", Table: &Table{State: state, Rows: []table.Row{{ID: "other", Cells: []string{"Other"}}}}})
	if first.TableSelection(state) != "two" || second.TableSelection(state) != "" || second.TableSelectedID(second.DialogID()) != "other" {
		t.Fatal("foreign table handle shared navigation or records")
	}
	if second.UpdateTable(firstDialog, "foreign", nil, nil) || second.SyncTableFocus(firstDialog, false) {
		t.Fatal("foreign dialog changed another controls owner")
	}
	if first.TableSelectedID(firstDialog) != "two" {
		t.Fatal("second owner mutated the first table")
	}
}
