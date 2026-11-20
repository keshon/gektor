package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestPickerFootersFitAcrossThemesAndTerminalSizes(t *testing.T) {
	defer setTheme("dark")
	root := t.TempDir()
	for _, theme := range config.ThemeNames() {
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}, {160, 50}} {
			for _, field := range []string{"run", "app-workspace"} {
				m := sizeModelTo(t, testModel(), size[0], size[1])
				m.applyTheme(theme)
				m.chrome.PresentForm(screen.Form{Title: "Choose", ApplyLabel: "Apply", Fields: []form.Field{{Key: field, Value: root}}, Choices: map[string][]string{"run": {"first", "second"}}, FileFields: map[string]fieldform.FilePicker{"app-workspace": {Directory: true}}})
				props := m.chrome.FormProps()
				m.chrome.FocusForm(0)
				m.chrome.OpenFormPicker(m.chrome.DialogID())
				items := make([]string, 80)
				for i := range items {
					items[i] = fmt.Sprintf("Choice %02d", i)
					if field == "app-workspace" {
						items[i] = filepath.Join(root, items[i]) + string(os.PathSeparator)
					}
				}
				m.chrome.AcceptFormScan(fieldform.ScanMsg{ID: testFormSnapshot(m).PickerID, Items: items})
				rows, hits, _, _ := m.chrome.FormRows()
				if len(rows)+2 > props.Height {
					t.Fatal("picker hid its footer below available area", theme, size, field, len(rows), props.Height)
				}
				if len(rows) < 3 || !strings.Contains(ansi.Strip(rows[len(rows)-3]), "────") || !strings.Contains(ansi.Strip(rows[len(rows)-2]), "ESC ") {
					t.Fatal("picker lost shared footer", theme, size, field)
				}
				if strings.Contains(ansi.Strip(rows[len(rows)-2]), "…") {
					t.Fatal("picker clipped its shortcut group", theme, size)
				}
				x, y := m.chrome.FormOrigin(m.chrome.DialogID())
				cancelled := false
				for _, hit := range hits {
					if hit.Index == -4 {
						m.chrome.DialogInput(tea.MouseClickMsg{X: x + hit.X0, Y: y + hit.Y0, Button: tea.MouseLeft})
						cancelled = true
						break
					}
				}
				if !cancelled || testFormSnapshot(m).PickerOpen || testFormSnapshot(m).EditorValue != root {
					t.Fatal("visible Cancel did not preserve parent draft", theme, size, field)
				}
			}
		}
	}
}

func TestPickerToolbarEnterMatchesFocusedCancel(t *testing.T) {
	m := sizeModelTo(t, testModel(), 120, 30)
	m.chrome.PresentForm(screen.Form{Title: "Choose run", ApplyLabel: "Apply", Fields: []form.Field{{Key: "run", Value: "old"}}, Choices: map[string][]string{"run": {"old", "new"}}})
	m.chrome.FocusForm(0)
	m.chrome.OpenFormPicker(m.chrome.DialogID())
	m.chrome.FormKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m.chrome.FormKey(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	for i, action := range m.footerActions() {
		if action.Key == tea.KeyEnter {
			if action.Label != "Cancel" || !action.Enabled {
				t.Fatal("toolbar describes the wrong Enter action")
			}
			hit := m.testControls().ToolbarLayout().Cells[i].Bounds
			m.handleMouse(tea.MouseClickMsg{X: hit.X, Y: m.termH - 1, Button: tea.MouseLeft})
			if m.chrome.DialogInfo().Kind == 0 || testFormSnapshot(m).PickerOpen || testFormSnapshot(m).EditorValue != "old" {
				t.Fatal("toolbar Enter did not cancel just the picker")
			}
			return
		}
	}
	t.Fatal("picker toolbar has no Enter action")
}
