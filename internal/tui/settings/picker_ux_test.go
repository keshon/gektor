package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

func TestWorkspacePickerNavigatesBeforeSelecting(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	f := NewModel([]form.Field{{Key: "app-workspace", Value: child}}, textarea.New())
	f.Focus(0, nil)
	props := Props{Width: 100, Height: 30}
	f.AcceptScan(f.OpenPicker(props)().(ScanMsg))
	rows, _, _, _ := f.PickerRows(props)
	if !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "...") || !strings.HasPrefix(rows[2], filepath.VolumeName(child)) || f.Picker.Directory != child {
		t.Fatal(rows)
	}
	x, y := f.Origin(props)
	_, cmd := f.Mouse(tea.MouseClickMsg{X: x + 2, Y: y + f.Picker.listTop(), Button: tea.MouseLeft}, props)
	if cmd == nil || f.Picker == nil || f.Editor.Value() != child {
		t.Fatal("parent click selected path")
	}
	f.AcceptScan(cmd().(ScanMsg))
	if f.Picker.Directory != root {
		t.Fatal(f.Picker.Directory)
	}
	_, hits, _, _ := f.PickerRows(props)
	x, y = f.Origin(props)
	for _, hit := range hits {
		if hit.Index == -2 {
			f.Mouse(tea.MouseClickMsg{X: x + 1, Y: y + hit.Y0, Button: tea.MouseLeft}, props)
		}
	}
	if f.Picker != nil || f.Editor.Value() != root {
		t.Fatal("explicit folder selection failed")
	}
}

func TestThemePickerIsCompactAndStartsAtCurrentValue(t *testing.T) {
	f := NewModel([]form.Field{{Key: "theme", Value: "light"}}, textarea.New())
	f.Choices = map[string][]string{"theme": {"dark", "light"}}
	f.Focus(0, nil)
	props := Props{Width: 140, Height: 45}
	f.OpenPicker(props)
	rows, _, width, _ := f.PickerRows(props)
	text := ansi.Strip(strings.Join(rows, "\n"))
	if len(rows) > 8 || width > 52 || strings.Contains(text, "Search") || strings.Contains(text, "drives") || f.Picker.Filtered[f.Picker.Cursor] != "light" {
		t.Fatal(text, width)
	}
	x, y := f.Origin(props)
	f.Mouse(tea.MouseClickMsg{X: x + 2, Y: y + f.Picker.listTop(), Button: tea.MouseLeft}, props)
	if f.Picker != nil || f.Editor.Value() != "dark" {
		t.Fatal("compact picker mouse coordinates diverged")
	}
}

func TestLongChoicePickerUsesStandardModalWidthAndMouseOrigin(t *testing.T) {
	items := []string{strings.Repeat("A", 26) + " · Agent workflow with a long name", strings.Repeat("B", 26) + " · Another saved workflow"}
	for _, width := range []int{140, 80, 40} {
		f := NewModel([]form.Field{{Key: "run", Value: items[1]}}, textarea.New())
		f.Choices = map[string][]string{"run": items}
		f.Focus(0, nil)
		props := Props{Width: width, Height: 30}
		f.OpenPicker(props)
		if actual := cell.Width(strings.Join(f.box(props), "\n")); actual != popup.ModalWidth(width) {
			t.Fatalf("width %d: chooser %d, expected standard %d", width, actual, popup.ModalWidth(width))
		}
		rows, _, _, _ := f.PickerRows(props)
		if width >= 80 && !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), "Agent workflow with a long name") {
			t.Fatal("wide chooser truncated a fitting name", rows)
		}
		x, y := f.Origin(props)
		f.Mouse(tea.MouseClickMsg{X: x + 2, Y: y + f.Picker.listTop(), Button: tea.MouseLeft}, props)
		if f.Picker != nil || f.Editor.Value() != items[0] {
			t.Fatal("wider picker click missed first choice")
		}
	}
}

func TestConfiguredFilePickerFiltersNavigatesAndSelects(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "recipes")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"recipe.JSON", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(child, name), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f := NewModel([]form.Field{{Key: "path"}}, textarea.New())
	f.FileFields = map[string]FilePicker{"path": {Root: root, Extensions: []string{".json"}}}
	f.Focus(0, nil)
	props := Props{Width: 100, Height: 30}
	f.AcceptScan(f.OpenPicker(props)().(ScanMsg))
	if !f.Picker.IsFiles() || f.Picker.Directory != root {
		t.Fatal("wrong picker root")
	}
	for i, item := range f.Picker.Filtered {
		if filepath.Clean(item) == child {
			f.Picker.Cursor = i
		}
	}
	f.AcceptScan(f.PickerKey(tea.KeyPressMsg{Code: tea.KeyEnter}, props)().(ScanMsg))
	if f.Picker.Directory != child || f.Editor.Value() != "" {
		t.Fatal("navigation selected a directory")
	}
	for i, item := range f.Picker.Filtered {
		if strings.HasSuffix(item, "notes.txt") {
			t.Fatal("unmatched extension shown")
		}
		if strings.HasSuffix(item, "recipe.JSON") {
			f.Picker.Cursor = i
		}
	}
	f.PickerKey(tea.KeyPressMsg{Code: tea.KeyEnter}, props)
	if f.Picker != nil || f.Editor.Value() != filepath.Join(child, "recipe.JSON") {
		t.Fatal("file not selected")
	}
}
