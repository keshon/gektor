package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSessionModalScrollbarAndClose(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"light", "dark"} {
		m := sizeModelTo(t, testModel(), 100, 34)
		m.applyTheme(theme)
		m.openSessions()
		m.sessions.entries = nil
		for i := 0; i < 12; i++ {
			m.sessions.entries = append(m.sessions.entries, sessionEntry{title: fmt.Sprintf("Session %d", i), preview: "Review changes and verify the implementation", msgs: 24})
		}
		g := m.searchGeometry()
		rows := strings.Split(ansi.Strip(m.View().Content), "\n")
		x, y := g.x+g.width-3, g.y+4
		if []rune(rows[y])[x] != '█' {
			t.Fatal("scrollbar does not match hit area", rows[y])
		}
		m.handleMouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		m.handleMouse(tea.MouseMotionMsg{X: x + 5, Y: y + 40, Button: tea.MouseLeft})
		m.handleMouse(tea.MouseReleaseMsg{X: x + 5, Y: y + 40, Button: tea.MouseLeft})
		if m.sessions.Offset != len(m.sessions.entries)-m.sessVisible() || m.sessions.drag.active {
			t.Fatal("thumb did not reach bottom or release", m.sessions.Offset)
		}
		if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
			if err := os.WriteFile(filepath.Join(dir, "sessions-"+theme+".ansi"), []byte(m.View().Content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		m.handleMouse(tea.MouseClickMsg{X: g.x + g.width - 4, Y: g.y + 1, Button: tea.MouseLeft})
		if m.sessions != nil {
			t.Fatal("close button missed")
		}
	}
}
