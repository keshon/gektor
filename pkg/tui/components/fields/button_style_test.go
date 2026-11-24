package fields

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/popup"
)

func TestChooserFieldsUseFilledButtons(t *testing.T) {
	fields := []form.Field{{Key: "folder", Label: "Workspace"}, {Key: "model", Label: "Model"}, {Key: "choice", Label: "Choice", Value: "one"}}
	f := NewModel(fields, textarea.New())
	f.ApplyLabel = "Apply"
	f.FileFields = map[string]FilePicker{"folder": {Root: t.TempDir(), Directory: true}}
	f.DiscoveryFields = map[string]string{"model": "models"}
	f.Choices = map[string][]string{"choice": {"one", "two"}}
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color("#eeeeee")).Background(lipgloss.Color("#111111"))
	props := Props{Width: 100, Height: 35, Surface: popup.Styles{Normal: normal}, Styles: Styles{
		FilledButtons: true, Focus: normal.Foreground(lipgloss.Color("#88dddd")), Button: normal.Background(lipgloss.Color("#334455")),
	}}
	for index := range fields {
		f.Focus(index, nil)
		rows, _, _, _ := f.Rows(props)
		view := strings.Join(rows, "\n")
		plain := ansi.Strip(view)
		for _, label := range []string{"Choose…", "Discover…", "one"} {
			if !strings.Contains(plain, label) {
				t.Fatal("chooser missing", index, label)
			}
		}
		if strings.ContainsAny(plain, "[]") {
			t.Fatal("filled chooser still uses bracketed controls", index, plain)
		}
	}
	f.Focus(0, nil)
	intent, _ := f.UpdateKey(tea.KeyPressMsg{Code: tea.KeyEnter}, props)
	if intent.Action != None || f.Picker == nil {
		t.Fatal("chooser activation changed", intent)
	}
}
