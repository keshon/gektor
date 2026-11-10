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
			p := m.tableProps()
			layout := m.dialog.table.model.Layout(p)
			if len(layout.Rows)+2 > p.Height || layout.Capacity < 1 {
				t.Fatal("MCP controls exceed available height", theme, size)
			}
			selected := m.dialog.table.model.Selected()
			m.handleKey(tea.KeyPressMsg{Code: 'm', Text: "m"})
			if !m.dialog.table.model.MoreOpen {
				t.Fatal("MCP More menu is unavailable", theme, size)
			}
			menu := m.dialog.table.model.Layout(m.tableProps()).Menu
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
				if err := os.WriteFile(path, []byte(m.View().Content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			m.handleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
			if m.dialog == nil || m.dialog.table.model.MoreOpen || m.dialog.table.model.Selected() != selected {
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
				if err := os.WriteFile(path, []byte(m.View().Content), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
