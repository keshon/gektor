package agent

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/keshon/gektor/internal/config"
)

func TestMenuMouseIgnoresOutsideWheelFrameAndDisabledRows(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		for _, workflow := range []bool{false, true} {
			m := sizeModelTo(t, testModel(), 120, 30)
			if workflow {
				m = workflowTestModel(t)
				sizeModelTo(t, m, 120, 30)
			}
			m.applyTheme(theme)
			m.header = headerState{global: true, focused: true, menu: "settings"}
			m.header.row = m.headerActionRow(0, 1)
			l := m.headerMenuLayout()
			before := m.header.row
			m.headerMouse(tea.MouseWheelMsg{X: 119, Y: 27, Button: tea.MouseWheelDown})
			if m.header.row != before {
				t.Fatal("outside wheel changed menu selection", theme, workflow)
			}
			m.headerMouse(tea.MouseClickMsg{X: l.X, Y: l.Y + 1, Button: tea.MouseLeft})
			if m.dialog != nil || m.header.menu != "settings" {
				t.Fatal("menu frame activated a command", theme, workflow)
			}
			// A disabled command uses disabled styling even if stale selection points at it.
			m.header.menu = "session"
			m.state = stRunning
			for i, item := range m.headerMenuItems() {
				if item.Enabled || item.Info || item.Separator {
					continue
				}
				m.header.row = i
				l = m.headerMenuLayout()
				y := l.Y + 1 + i - l.Offset
				screen := uv.NewScreenBuffer(m.termW, m.termH)
				uv.NewStyledString(m.overlayHeader(m.View().Content)).Draw(screen, uv.Rect(0, 0, m.termW, m.termH))
				if c := screen.CellAt(l.X+3, y); c == nil || !sameColor(c.Style.Fg, themeColor(colorDisabledText)) {
					t.Fatal("disabled menu row looks selected", theme, workflow)
				}
				m.headerMouse(tea.MouseClickMsg{X: l.X + 3, Y: y, Button: tea.MouseLeft})
				if m.header.menu == "" || m.dialog != nil {
					t.Fatal("disabled menu row executed", theme, workflow)
				}
				break
			}
		}
	}
}

func TestMenuStructureAndActionOwnership(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		m := settingsModel(t)
		if workflow {
			m = workflowTestModel(t)
			sizeModelTo(t, m, 120, 30)
		}
		var labels []string
		for _, menu := range m.menuDefinitions() {
			labels = append(labels, menu.label)
		}
		want := []string{"GEKTOR", "Session", "Run", "View", "Settings", "Help"}
		if workflow {
			want = []string{"GEKTOR", "Recipe", "Node", "Run", "View", "Settings", "Help"}
		}
		if !reflect.DeepEqual(labels, want) {
			t.Fatal(labels)
		}
		owners := map[string]string{}
		for _, menu := range m.menuDefinitions() {
			for _, item := range m.menuItems(menu.id) {
				if item.command == "" {
					continue
				}
				if previous := owners[item.command]; previous != "" {
					t.Fatalf("%s owned by both %s and %s", item.command, previous, menu.id)
				}
				owners[item.command] = menu.id
			}
		}
		for command, owner := range map[string]string{"workspace": "app", "quit": "app", "appearance": "settings", "permissions-info": "settings", "prompt-info": "help"} {
			// Minimal workflow fixtures have no model configuration.
			if command == "prompt-info" && m.config == nil {
				continue
			}
			if owners[command] != owner {
				t.Fatalf("%s belongs in %s, got %s", command, owner, owners[command])
			}
		}
		if !workflow && owners["toggle-steering"] != "execution" {
			t.Fatal("steering is not a run control")
		}
	}
}

func TestNarrowMenuOverflowReachesEveryMenu(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		for _, width := range []int{24, 40, 80, 120} {
			m := sizeModelTo(t, testModel(), width, 24)
			if workflow {
				m = workflowTestModel(t)
				sizeModelTo(t, m, width, 24)
			}
			reached := map[string]bool{}
			for _, cell := range m.menuCells() {
				if cell.end > width {
					t.Fatal("menu outside terminal", width, cell)
				}
				if cell.action.name != "overflow" {
					reached[cell.action.name] = true
					continue
				}
				m.header = headerState{global: true, focused: true, menu: "overflow"}
				items := m.headerItems()
				for i, item := range items {
					m.header.menu, m.header.row = "overflow", i
					m.headerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
					target := strings.TrimPrefix(item.command, "menu:")
					if m.header.menu != target || !m.header.global {
						t.Fatal("overflow target did not open", target)
					}
					layout := m.headerMenuLayout()
					if layout.X < 0 || layout.X+layout.Width > width {
						t.Fatal("overflow popup outside terminal", width)
					}
					reached[target] = true
				}
			}
			for _, menu := range m.menuDefinitions() {
				if !reached[menu.id] {
					t.Fatal("unreachable menu", width, menu.id)
				}
			}
		}
	}
}

func TestAppearanceStagesAndSavesThemeAndFrameTogether(t *testing.T) {
	defer setTheme("dark")
	m := settingsModel(t)
	before, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil {
		t.Fatal(err)
	}
	initial := m.currentAppearance()
	m.openAppearance()
	focusSetting(m, "theme")
	m.dialog.form.Editor.SetValue("light")
	focusSetting(m, "frame")
	m.dialog.form.Editor.SetValue("minimal")
	m.formKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	after, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil || !reflect.DeepEqual(before, after) || m.currentAppearance() != initial {
		t.Fatal("Cancel changed appearance")
	}
	m.openAppearance()
	focusSetting(m, "theme")
	m.dialog.form.Editor.SetValue("light")
	focusSetting(m, "frame")
	m.dialog.form.Editor.SetValue("minimal")
	m.applyForm()
	if m.dialog != nil {
		t.Fatal("Appearance did not apply", m.dialog.form.Error)
	}
	after, err = config.LoadPreferences(m.application.PreferencePath)
	if err != nil || after.Theme != "light" || after.Appearance.Frame != "minimal" || !m.minimal {
		t.Fatal("appearance not persisted", after, err)
	}
	if after.Workspace != before.Workspace || !reflect.DeepEqual(after.Profile, before.Profile) {
		t.Fatal("Appearance changed setup")
	}
}

func TestMenuModeSwitchPreservesAgentDraft(t *testing.T) {
	m := workflowTestModel(t)
	m.input.SetValue("unfinished agent message")
	m.activateMenuAction("screen:agent")
	if m.workflowActive() || m.input.Value() != "unfinished agent message" {
		t.Fatal("return to Agent lost draft")
	}
	m.activateMenuAction("screen:workflows")
	if !m.workflowActive() || m.input.Value() != "unfinished agent message" {
		t.Fatal("opening Workflows lost draft")
	}
}

func TestCompactMenuRequiresExplicitConfirmation(t *testing.T) {
	m := settingsModel(t)
	m.activateMenuAction("compact-session")
	if m.dialog == nil || m.dialog.form == nil || m.dialog.form.Index != 1 {
		t.Fatal("compaction did not start on Cancel")
	}
	m.formKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.dialog != nil {
		t.Fatal("compaction could not be cancelled")
	}
}
