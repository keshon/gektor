package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/roles"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestSettingsMenuRowsAreActionable(t *testing.T) {
	for _, mode := range []string{"screen:agent", "screen:workflows"} {
		for _, width := range []int{40, 80, 120} {
			m := settingsModel(t)
			m.runAction(mode)
			sizeModelTo(t, m, width, 30)
			m.runAction("settings")
			m.syncPresentation()
			for _, item := range m.chrome.HeaderItems() {
				if item.Text != "" && item.Command == "" {
					t.Fatalf("non-actionable Settings row looks like a menu item: %q", item.Text)
				}
			}
			clickVisible(t, m, "Appearance…")
			if m.chrome.Dialog == nil || m.chrome.Dialog.Title != "Appearance" {
				t.Fatalf("Appearance click failed in %s at width %d", mode, width)
			}
		}
	}
}

func TestMenuStructureAndActionOwnership(t *testing.T) {
	m := settingsModel(t)

	var labels []string
	for _, menu := range m.chrome.Props.Menus {
		labels = append(labels, menu.Label)
	}
	want := []string{"GEKTOR", "Session", "Run", "View", "Settings", "Help"}

	if !reflect.DeepEqual(labels, want) {
		t.Fatal(labels)
	}
	owners := map[string]string{}
	for _, menu := range m.chrome.Props.Menus {
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
	for command, owner := range map[string]string{"workspace": "app", "quit": "app", "appearance": "settings", "permissions-info": "execution", "prompt-info": "help"} {
		// This fixture has no model configuration.
		if command == "prompt-info" && m.profile == nil {
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

func TestGlobalDropdownNavigationAndSettings(t *testing.T) {
	m := settingsModel(t)

	line := ansi.Strip(m.chrome.MenuLine())
	if strings.Count(line, "GEKTOR") != 1 || strings.Contains(line, "F1") || !strings.Contains(line, "Settings") {
		t.Fatal(line)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF9})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chrome.Header.Menu != "app" {
		t.Fatal(m.chrome.Header)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.chrome.Header.Menu != "execution" {
		t.Fatal("left/right failed to switch dropdown")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if snapshotOf(m).KeepAct || m.chrome.Header.Focused {
		t.Fatal("Plan selection failed")
	}
	var settings screen.ActionCell
	for _, c := range m.chrome.MenuCells() {
		if c.Action.Name == "settings" {
			settings = c
			break
		}
	}
	m.Update(tea.MouseClickMsg{X: settings.Start, Y: 0, Button: tea.MouseLeft})
	if m.chrome.Header.Menu != "settings" || m.chrome.Header.Row != m.chrome.HeaderActionRow(0, 1) {
		t.Fatal("settings page failed")
	}
}

// Brand + clock ride the header: animated letter while running,
// solid amber word at rest.
func TestMenuBrandAndHeaderWidth(t *testing.T) {
	m := shellTestModel(t, Config{AgentConfig: AgentConfig{Task: "wait", Env: roles.Env{Client: stoppedClient{}}}})

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	mm := updated.(*Model)
	var header string
	for _, ln := range strings.Split(mm.View().Content, "\n") {
		if strings.Contains(ln, "Settings") {
			header = ln
		}
	}
	if header == "" {
		t.Fatal("header missing brand line")
	}
	if n := lipgloss.Width(header); n != 160 {
		t.Fatalf("header width = %d, want 160 (clock at right edge)", n)
	}
	// Fixed two-line zone: the blocked input stays visible while
	// running (empty transcript here, so "> " can only be the box).
	if !strings.Contains(mm.View().Content, "> ") {
		t.Fatal("running must keep the blocked input box")
	}
	mm = settingsModel(t)
	updated, _ = mm.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	// Brand owns GEKTOR alone: exactly one occurrence on the row.
	if view := strings.Split(updated.(*Model).View().Content, "\n")[0]; strings.Count(view, "GEKTOR") != 1 {
		t.Fatalf("done header must show one brand: %q", view)
	}
}
func TestAboutMenuAndCommandShareContent(t *testing.T) {
	m := settingsModel(t)
	m.command("/about")
	expected := strings.Join(m.chrome.Dialog.Lines, "\n")
	m.closeDialog()
	m.chrome.Header.Focused = true
	m.chrome.Header.Menu = "help"
	m.chrome.Header.Row = headerRowFor(m, "about")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chrome.Dialog == nil || m.chrome.Dialog.Title != "GEKTOR" || strings.Join(m.chrome.Dialog.Lines, "\n") != expected {
		t.Fatalf("About menu and command disagree: %+v", m.chrome.Dialog)
	}
}
