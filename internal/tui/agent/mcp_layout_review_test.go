package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/mcpmanager"
)

func TestMCPReviewLayouts(t *testing.T) {
	defer setTheme("dark")
	if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	manager, err := mcpmanager.New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	for _, server := range []mcpmanager.Server{
		{ID: "yougile", Description: "Project tasks", URL: "https://example.com/tasks", Enabled: true},
		{ID: "browser", Description: "Browser tools", URL: "https://example.com/browser", Enabled: true, Startup: true},
		{ID: "notes", Description: "Local notes", URL: "https://example.com/notes"},
	} {
		if err := manager.Save(server); err != nil {
			t.Fatal(err)
		}
	}
	for _, theme := range config.ThemeNames() {
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}} {
			m := sizeModelTo(t, testModel(), size[0], size[1])
			m.mcp, m.state = manager, stDone
			m.applyTheme(theme)
			m.openMCP()
			p := m.testControls().TableProps()
			layout := m.chrome.Dialog.Table.Model.Layout(p)
			if len(layout.Rows)+2 > p.Height || layout.Capacity < 1 {
				t.Fatal("MCP controls exceed available height", theme, size)
			}
			selected := m.chrome.Dialog.Table.Model.Selected()
			m.testKey(tea.KeyPressMsg{Code: 'm', Text: "m"})
			if !m.chrome.Dialog.Table.Model.MoreOpen {
				t.Fatal("MCP More menu is unavailable", theme, size)
			}
			menu := m.chrome.Dialog.Table.Model.Layout(m.testControls().TableProps()).Menu
			screen := uv.NewScreenBuffer(menu.Width, len(menu.Rows))
			uv.NewStyledString(strings.Join(menu.Rows, "\n")).Draw(screen, uv.Rect(0, 0, menu.Width, len(menu.Rows)))
			for row, text := range menu.Rows {
				if strings.Contains(ansi.Strip(text), "Delete") {
					cell := screen.CellAt(3, row)
					if cell == nil || !sameColor(cell.Style.Fg, themeColor(colorError)) || !sameColor(cell.Style.Bg, themeColor(colorPopupBackground)) {
						t.Fatal("destructive menu action lost its danger color or surface", theme, size)
					}
				}
			}
			if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
				path := filepath.Join(dir, fmt.Sprintf("mcp-more-%s-%dx%d.ansi", theme, size[0], size[1]))
				if err := os.WriteFile(path, []byte(m.testView().Content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			m.testKey(tea.KeyPressMsg{Code: tea.KeyEsc})
			if m.chrome.Dialog == nil || m.chrome.Dialog.Table.Model.MoreOpen || m.chrome.Dialog.Table.Model.Selected() != selected {
				t.Fatal("returning from More lost the server list")
			}
			for _, row := range layout.Rows {
				if ansi.StringWidth(row) > layout.Width-4 {
					t.Fatal("MCP content exceeds frame width", theme, size)
				}
			}
			if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, fmt.Sprintf("mcp-%s-%dx%d.ansi", theme, size[0], size[1]))
				if err := os.WriteFile(path, []byte(m.testView().Content), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
