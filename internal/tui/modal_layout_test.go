package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

func TestModalsShareFrameAndPreserveFullWidthDraft(t *testing.T) {
	for _, width := range []int{40, 80, 120, 160} {
		m := sizeModelTo(t, testModel(), width, 34)
		m.state = stDone
		m.input.SetValue("draft\nsecond line")
		m.fitBottom()
		if m.transcript.Viewport.Width() != width-2 {
			t.Fatal("conversation does not use the full width")
		}
		m.openDialog("About", []string{"GEKTOR"})
		checkModalWidth(t, m.dialogView(), popup.ModalWidth(width-2))
		m.closeDialog()
		m.openForm("Rename session", "Change the title.", "Rename", []formField{{Key: "title", Label: "Title", Value: "Task"}}, func(map[string]string) error { return nil })
		checkModalWidth(t, m.formView(), popup.ModalWidth(width-2))
		m.closeDialog()
		m.openSessions()
		m.sessions.entries = []sessionEntry{{title: "Repair startup", updated: time.Now().Add(-49 * time.Hour), msgs: 12, preview: "Check cancellation and process ownership"}, {title: "Review changes", updated: time.Now(), msgs: 3}}
		m.fitBottom()
		checkModalWidth(t, m.searchView(), popup.ModalWidth(width-2))
		g := m.searchGeometry()
		if lipgloss.Height(m.searchView()) != g.height {
			t.Fatal("session rendering and hit-test geometry disagree")
		}
		text := ansi.Strip(m.searchView())
		if width >= 80 && (!strings.Contains(text, "2d ago") || !strings.Contains(text, "12 messages")) {
			t.Fatal("session metadata missing")
		}
		m.browserMouse(tea.MouseClickMsg{X: g.x + 2, Y: g.y + 7, Button: tea.MouseLeft})
		if m.sessions.Cursor != 0 {
			t.Fatal("separator selected a session")
		}
		m.browserMouse(tea.MouseClickMsg{X: g.x + 2, Y: g.y + 8, Button: tea.MouseLeft})
		if m.sessions.Cursor != 1 {
			t.Fatal("second session could not be selected")
		}
		if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" && width == 120 {
			if err := os.WriteFile(filepath.Join(dir, "open-session.txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
				t.Fatal(err)
			}
		}
		m.sessionsKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
		m.syncFocus()
		g = m.searchGeometry()
		rows := strings.Split(ansi.Strip(m.View().Content), "\n")
		if cursor := m.View().Cursor; cursor == nil || cursor.Y != g.noteRow || !strings.Contains(rows[g.noteRow], "name>") {
			t.Fatal("rename caret does not match the rendered input")
		}
		m.closeSessions()
		if m.input.Value() != "draft\nsecond line" || !m.input.Focused() {
			t.Fatal("modal lost the draft or input focus")
		}
	}
}

func checkModalWidth(t *testing.T, view string, width int) {
	t.Helper()
	for _, row := range strings.Split(ansi.Strip(view), "\n") {
		if strings.Contains(row, "╭") || strings.Contains(row, "╰") {
			if lipgloss.Width(strings.TrimLeft(row, " ")) != width {
				t.Fatalf("modal frame width: %q, want %d", row, width)
			}
		}
	}
}
