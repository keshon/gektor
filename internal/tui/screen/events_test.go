package screen

import (
	"errors"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/settings"
	"testing"
)

func TestFormCopiesInputsAndSubmissionSnapshot(t *testing.T) {
	controls := &Controls{}
	fields := []form.Field{{Key: "name", Value: "original"}}
	choices := map[string][]string{"name": {"original", "other"}}
	files := map[string]settings.FilePicker{"path": {Extensions: []string{".json"}}}
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
	if len(events) != 1 || events[0].Kind != Apply || events[0].Dialog != dialog || events[0].Values["name"] != "original" {
		t.Fatal("submission did not capture the producing draft", events)
	}
	if controls.Dialog != dialog {
		t.Fatal("controls applied or closed an unanswered submission")
	}
}

func TestReplacementIgnoresLateFormAndSaveReplies(t *testing.T) {
	controls := &Controls{}
	controls.PresentForm(Form{Title: "First"})
	first := controls.Dialog
	controls.PresentForm(Form{Title: "Second"})
	second := controls.Dialog
	controls.CompleteForm(first, errors.New("late validation"))
	controls.CompleteForm(first, nil)
	controls.CompleteSave(first, nil)
	if controls.Dialog != second || second.Form.Error != "" {
		t.Fatal("late reply changed the replacement")
	}

	events := controls.TakeEvents()
	if len(events) != 3 || events[0].Kind != Opened || !events[0].First || events[1].Kind != Replaced || events[1].Dialog != first || events[2].First {
		t.Fatal("replacement broke overlay lifetime", events)
	}
	controls.Close()
	controls.Close()
	events = controls.TakeEvents()
	if len(events) != 1 || events[0].Kind != Closed || events[0].Dialog != second {
		t.Fatal("dismissal was not reported exactly once", events)
	}
}
