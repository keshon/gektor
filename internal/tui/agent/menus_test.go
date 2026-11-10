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
		m := sizeModelTo(t, testModel(), 120, 30)

		m.applyTheme(theme)
		m.chrome.Header = headerState{Global: true, Focused: true, Menu: "settings"}
		m.chrome.Header.Row = m.headerActionRow(0, 1)
		l := m.headerMenuLayout()
		before := m.chrome.Header.Row
		m.headerMouse(tea.MouseWheelMsg{X: 119, Y: 27, Button: tea.MouseWheelDown})
		if m.chrome.Header.Row != before {
			t.Fatal("outside wheel changed menu selection", theme)
		}
		m.headerMouse(tea.MouseClickMsg{X: l.X, Y: l.Y + 1, Button: tea.MouseLeft})
		if m.chrome.Dialog != nil || m.chrome.Header.Menu != "settings" {
			t.Fatal("menu frame activated a command", theme)
		}
		// A disabled command uses disabled styling even if stale selection points at it.
		m.chrome.Header.Menu = "session"
		m.state = stRunning
		for i, item := range m.headerMenuItems() {
			if item.Enabled || item.Info || item.Separator {
				continue
			}
			m.chrome.Header.Row = i
			l = m.headerMenuLayout()
			y := l.Y + 1 + i - l.Offset
			screen := uv.NewScreenBuffer(m.termW, m.termH)
			uv.NewStyledString(m.overlayHeader(m.View().Content)).Draw(screen, uv.Rect(0, 0, m.termW, m.termH))
			if c := screen.CellAt(l.X+3, y); c == nil || !sameColor(c.Style.Fg, themeColor(colorDisabledText)) {
				t.Fatal("disabled menu row looks selected", theme)
			}
			m.headerMouse(tea.MouseClickMsg{X: l.X + 3, Y: y, Button: tea.MouseLeft})
			if m.chrome.Header.Menu == "" || m.chrome.Dialog != nil {
				t.Fatal("disabled menu row executed", theme)
			}
			break
		}
	}
}

func TestMenuStructureAndActionOwnership(t *testing.T) {
	m := settingsModel(t)

	var labels []string
	for _, menu := range m.menuDefinitions() {
		labels = append(labels, menu.Label)
	}
	want := []string{"GEKTOR", "Session", "Run", "View", "Settings", "Help"}

	if !reflect.DeepEqual(labels, want) {
		t.Fatal(labels)
	}
	owners := map[string]string{}
	for _, menu := range m.menuDefinitions() {
		for _, item := range m.menuItems(menu.ID) {
			if item.Command == "" {
				continue
			}
			if previous := owners[item.Command]; previous != "" {
				t.Fatalf("%s owned by both %s and %s", item.Command, previous, menu.ID)
			}
			owners[item.Command] = menu.ID
		}
	}
	for command, owner := range map[string]string{"workspace": "app", "quit": "app", "appearance": "settings", "permissions-info": "settings", "prompt-info": "help"} {
		// This fixture has no model configuration.
		if command == "prompt-info" && m.config == nil {
			continue
		}
		if owners[command] != owner {
			t.Fatalf("%s belongs in %s, got %s", command, owner, owners[command])
		}
	}
	if owners["toggle-steering"] != "execution" {
		t.Fatal("steering is not a run control")
	}
}

func TestNarrowMenuOverflowReachesEveryMenu(t *testing.T) {
	for _, width := range []int{24, 40, 80, 120} {
		m := sizeModelTo(t, testModel(), width, 24)

		reached := map[string]bool{}
		for _, cell := range m.menuCells() {
			if cell.End > width {
				t.Fatal("menu outside terminal", width, cell)
			}
			if cell.Action.Name != "overflow" {
				reached[cell.Action.Name] = true
				continue
			}
			m.chrome.Header = headerState{Global: true, Focused: true, Menu: "overflow"}
			items := m.headerItems()
			for i, item := range items {
				m.chrome.Header.Menu, m.chrome.Header.Row = "overflow", i
				m.headerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
				target := strings.TrimPrefix(item.Command, "menu:")
				if m.chrome.Header.Menu != target || !m.chrome.Header.Global {
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
			if !reached[menu.ID] {
				t.Fatal("unreachable menu", width, menu.ID)
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
	m.chrome.Dialog.Form.Editor.SetValue("light")
	focusSetting(m, "frame")
	m.chrome.Dialog.Form.Editor.SetValue("minimal")
	m.formKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	after, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil || !reflect.DeepEqual(before, after) || m.currentAppearance() != initial {
		t.Fatal("Cancel changed appearance")
	}
	m.openAppearance()
	focusSetting(m, "theme")
	m.chrome.Dialog.Form.Editor.SetValue("light")
	focusSetting(m, "frame")
	m.chrome.Dialog.Form.Editor.SetValue("minimal")
	m.applyForm()
	if m.chrome.Dialog != nil {
		t.Fatal("Appearance did not apply", m.chrome.Dialog.Form.Error)
	}
	after, err = config.LoadPreferences(m.application.PreferencePath)
	if err != nil || after.Theme != "light" || after.Appearance.Frame != "minimal" || !m.minimal {
		t.Fatal("appearance not persisted", after, err)
	}
	if after.Workspace != before.Workspace || !reflect.DeepEqual(after.Profile, before.Profile) {
		t.Fatal("Appearance changed setup")
	}
}

func TestCompactMenuRequiresExplicitConfirmation(t *testing.T) {
	m := settingsModel(t)
	m.activateMenuAction("compact-session")
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil || m.chrome.Dialog.Form.Index != 1 {
		t.Fatal("compaction did not start on Cancel")
	}
	m.formKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.chrome.Dialog != nil {
		t.Fatal("compaction could not be cancelled")
	}
}
