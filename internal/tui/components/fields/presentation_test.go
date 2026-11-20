package fields

import (
	"testing"

	"charm.land/bubbles/v2/textarea"

	"github.com/keshon/gektor/internal/tui/components/form"
)

func TestPresentationRefreshKeepsEditorCursorAndPicker(t *testing.T) {
	f := NewModel([]form.Field{{Key: "name", Value: "unchanged"}}, textarea.New())
	f.Focus(0, nil)
	f.Editor.CursorStart()
	f.Configure(Presentation{Values: map[string]string{"name": "unchanged"}, Choices: map[string][]string{"name": {"unchanged", "other"}}})
	if f.Index != 0 || f.Editor.Column() != 0 || f.Editor.Value() != "unchanged" {
		t.Fatal("unchanged presentation reset editor navigation")
	}
	f.OpenPicker(Props{Width: 80, Height: 24})
	id := f.Picker.ID
	f.Picker.Search.SetValue("oth")
	f.Picker.Filter()
	f.Configure(Presentation{Choices: map[string][]string{"name": {"unchanged", "other"}}})
	if f.Picker.ID != id || f.Picker.Search.Value() != "oth" || len(f.Picker.Filtered) != 1 {
		t.Fatal("presentation replaced the active picker or its search")
	}
}

func TestPresentationAndSnapshotDoNotShareMutableStorage(t *testing.T) {
	f := NewModel([]form.Field{{Key: "path", Value: "original"}}, textarea.New())
	p := Presentation{
		Labels:      map[string]map[string]string{"path": {"original": "label"}},
		FileFields:  map[string]FilePicker{"path": {Extensions: []string{".json"}}},
		FieldErrors: map[string]string{"path": "invalid"},
	}
	f.Configure(p)
	p.Labels["path"]["original"] = "changed"
	p.FileFields["path"].Extensions[0] = ".other"
	p.FieldErrors["path"] = "changed"
	if f.Labels["path"]["original"] != "label" || f.FileFields["path"].Extensions[0] != ".json" || f.FieldErrors["path"] != "invalid" {
		t.Fatal("form retained caller presentation storage")
	}
	snapshot := f.Snapshot()
	snapshot.Fields[0].Value = "changed"
	snapshot.FieldErrors["path"] = "changed"
	if f.Fields[0].Value != "original" || f.FieldErrors["path"] != "invalid" {
		t.Fatal("snapshot exposed the form's mutable storage")
	}
}

func TestPickerAndChoicesSnapshotsAreDetached(t *testing.T) {
	f := NewModel([]form.Field{{Key: "choice", Value: "one"}}, textarea.New())
	f.Choices = map[string][]string{"choice": {"one", "two"}}
	f.Focus(0, nil)
	f.OpenPicker(Props{Width: 80, Height: 24})
	items := []string{"one", "two"}
	f.AcceptScan(ScanMsg{ID: f.Picker.ID, Items: items})
	items[0] = "caller changed scan"
	snapshot := f.Snapshot()
	snapshot.Choices["choice"][0] = "caller changed choices"
	snapshot.Picker.Items[0] = "caller changed items"
	snapshot.Picker.Filtered[0] = "caller changed filter"
	snapshot.Picker.Search = "caller changed search"
	current := f.Snapshot()
	if current.Choices["choice"][0] != "one" || current.Picker.Items[0] != "one" || current.Picker.Filtered[0] != "one" || current.Picker.Search != "" {
		t.Fatal("snapshot or scan result shares picker storage")
	}
}
