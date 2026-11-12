package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/mcpmanager"
	"github.com/keshon/gektor/internal/permission"
)

func TestPermissionsFilterKeepsRuleOrderAndFullDetails(t *testing.T) {
	m := sizeModelTo(t, testModel(), 100, 34)
	m.state = stDone
	m.input.SetValue("unfinished draft")
	m.policyDetails = []permission.Rule{{Tool: "*", Pattern: "*", Effect: permission.Allow},
		{Tool: "read_file", Pattern: "*.env · keep this full pattern", Effect: permission.Ask},
		{Tool: "run_shell", Pattern: "rm -rf", Effect: permission.Ask}}
	m.openPermissions()
	m.testKey(tea.KeyPressMsg{Code: '/'})
	m.testUpdate(tea.PasteMsg{Content: "read_file"})
	view := m.chrome.Dialog.Table.Model
	if view.Selected() != "1" || len(view.Rows) != 3 {
		t.Fatal("filter reordered or changed policy rules")
	}
	text := ansi.Strip(m.testView().Content)
	if !strings.Contains(text, "Later matching rules take precedence") || !strings.Contains(text, "read_file") {
		t.Fatal("precedence or filtered rule missing")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chrome.Dialog.Table != nil || !strings.Contains(strings.Join(m.chrome.Dialog.Lines, "\n"), "*.env · keep this full pattern") {
		t.Fatal("Details lost the full rule")
	}
	m.chrome.Close()
	m.syncControls()
	msg := m.pendingCommands[len(m.pendingCommands)-1]()
	m.testUpdate(msg)
	if m.chrome.Dialog.Table == nil || m.chrome.Dialog.Table.Model.Search.Value() != "read_file" || m.chrome.Dialog.Table.Model.Selected() != "1" {
		t.Fatal("returning from Details lost table state")
	}
	m.chrome.Close()
	m.syncControls()
	if m.input.Value() != "unfinished draft" || len(m.overlays) != 0 {
		t.Fatal("table navigation lost conversation draft or leaked overlays")
	}
}

func TestManagementTablesFitAllThemesAndSizes(t *testing.T) {
	defer setTheme("dark")
	root := t.TempDir()
	manager, err := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if err := manager.Save(mcpmanager.Server{ID: "reference", Description: "Reference tools", URL: "https://example.com/mcp", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, theme := range config.ThemeNames() {
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}} {
			m := sizeModelTo(t, testModel(), size[0], size[1])
			m.applyTheme(theme)
			m.policyDetails = []permission.Rule{{Tool: "*", Pattern: "*", Effect: permission.Allow},
				{Tool: "read_file", Pattern: "*.env", Effect: permission.Ask},
				{Tool: "run_shell", Pattern: "rm -rf", Effect: permission.Ask}}
			m.openPermissions()
			if !strings.Contains(ansi.Strip(m.testControls().DialogView()), "Later matching rules") {
				t.Fatal("permission precedence was hidden by compact layout", theme, size)
			}
			rows, width, height := m.testControls().DialogRows()
			if len(rows)+2 > height {
				t.Fatalf("%s %v: table exceeds pane", theme, size)
			}
			checkModalWidth(t, m.testControls().DialogView(), min(80, width-2))
			if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" && size[0] == 120 {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "permissions-"+theme+".ansi"), []byte(m.testView().Content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p := m.testControls().TableProps()
			l := m.chrome.Dialog.Table.Model.Layout(p)
			left, top, _, _ := m.dialogArea()
			for _, hit := range l.Hits {
				if hit.Action == "close" {
					m.handleMouse(tea.MouseClickMsg{X: left + max((p.Width-l.Width)/2, 0) + 2 + hit.X0,
						Y: top + max((p.Height-len(l.Rows)-2)/2, 0) + 1 + hit.Y, Button: tea.MouseLeft})
					break
				}
			}
			if m.chrome.Dialog != nil {
				t.Fatal("table trapped focus")
			}
			m.mcp, m.state = manager, stDone
			m.openMCP()
			rows, _, height = m.testControls().DialogRows()
			if len(rows)+2 > height {
				t.Fatalf("%s %v: MCP table exceeds pane", theme, size)
			}
			m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.chrome.Dialog != nil {
				t.Fatal("MCP table trapped focus")
			}
		}
	}
}

func TestReplacingDialogReusesOverlayFrame(t *testing.T) {
	m := sizeModelTo(t, testModel(), 100, 34)
	m.testControls().Open("Connecting", nil)
	m.testControls().Open("Connected", nil)
	if len(m.overlays) != 1 {
		t.Fatal("replacing a modal stacked an invisible overlay")
	}
	m.chrome.Close()
	m.syncControls()
	if len(m.overlays) != 0 {
		t.Fatal("close did not release overlay")
	}
}

func TestReturningToTableDoesNotReplaceAnotherDialog(t *testing.T) {
	m := sizeModelTo(t, testModel(), 100, 34)
	m.state = stDone
	m.policyDetails = []permission.Rule{{Tool: "*", Pattern: "*", Effect: permission.Allow}}
	m.openPermissions()
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.chrome.Close()
	m.syncControls()
	msg := m.pendingCommands[len(m.pendingCommands)-1]()
	m.testControls().Open("Another dialog", nil)
	m.testUpdate(msg)
	if m.chrome.Dialog.Title != "Another dialog" {
		t.Fatal("late table return replaced a newer dialog")
	}
}
