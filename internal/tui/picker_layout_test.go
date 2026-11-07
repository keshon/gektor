package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/settings"
)

func TestPickerFootersFitAcrossThemesAndTerminalSizes(t *testing.T) {
	defer setTheme("dark")
	root := t.TempDir()
	for _, theme := range config.ThemeNames() {
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}, {160, 50}} {
			for _, field := range []string{"run", "app-workspace"} {
				m := sizeModelTo(t, testModel(), size[0], size[1])
				m.applyTheme(theme)
				m.openForm("Choose", "", "Apply", []formField{{Key: field, Value: root}}, func(map[string]string) error { return nil })
				f, props := m.dialog.form, m.settingsProps()
				f.Choices = map[string][]string{"run": {"first", "second"}}
				f.Focus(0, nil)
				f.OpenPicker(props)
				items := make([]string, 80)
				for i := range items {
					items[i] = fmt.Sprintf("Choice %02d", i)
					if field == "app-workspace" {
						items[i] = filepath.Join(root, items[i]) + string(os.PathSeparator)
					}
				}
				f.AcceptScan(settings.ScanMsg{ID: f.Picker.ID, Items: items})
				rows, hits, _, _ := f.PickerRows(props)
				if len(rows)+2 > props.Height {
					t.Fatal("picker hid its footer below available area", theme, size, field, len(rows), props.Height)
				}
				if len(rows) < 3 || !strings.Contains(ansi.Strip(rows[len(rows)-3]), "────") || !strings.Contains(ansi.Strip(rows[len(rows)-2]), "Esc ") {
					t.Fatal("picker lost shared footer", theme, size, field)
				}
				if strings.Contains(ansi.Strip(rows[len(rows)-2]), "…") {
					t.Fatal("picker clipped its shortcut group", theme, size)
				}
				x, y := f.Origin(props)
				cancelled := false
				for _, hit := range hits {
					if hit.Index == -4 {
						f.Mouse(tea.MouseClickMsg{X: x + hit.X0, Y: y + hit.Y0, Button: tea.MouseLeft}, props)
						cancelled = true
						break
					}
				}
				if !cancelled || f.Picker != nil || f.Editor.Value() != root {
					t.Fatal("visible Cancel did not preserve parent draft", theme, size, field)
				}
			}
		}
	}
}

func TestPickerToolbarEnterMatchesFocusedCancel(t *testing.T) {
	m := sizeModelTo(t, testModel(), 120, 30)
	m.openForm("Choose run", "", "Apply", []formField{{Key: "run", Value: "old"}}, func(map[string]string) error { return nil })
	f := m.dialog.form
	f.Choices = map[string][]string{"run": {"old", "new"}}
	f.Focus(0, nil)
	f.OpenPicker(m.settingsProps())
	f.Picker.Cursor, f.Picker.Control = 1, 2
	for i, action := range m.footerActions() {
		if action.key == tea.KeyEnter {
			if action.label != "Cancel" || !action.enabled {
				t.Fatal("toolbar describes the wrong Enter action")
			}
			hit := m.toolbarLayout().Cells[i].Bounds
			m.handleMouse(tea.MouseClickMsg{X: hit.X, Y: m.termH - 1, Button: tea.MouseLeft})
			if m.dialog == nil || f.Picker != nil || f.Editor.Value() != "old" {
				t.Fatal("toolbar Enter did not cancel just the picker")
			}
			return
		}
	}
	t.Fatal("picker toolbar has no Enter action")
}
