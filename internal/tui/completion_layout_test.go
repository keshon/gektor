package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
)

func TestCompletionMouseSelectsWithoutRunningAndClosePreservesDraft(t *testing.T) {
	m := completionModel(t)
	m.input.SetValue("/")
	m.syncSuggestions()
	l, x, y := m.suggestionLayout()
	m.handleMouse(tea.MouseClickMsg{X: x + 2, Y: y + l.BodyTop + 1, Button: tea.MouseLeft})
	if m.suggestions.Selected != 1 || m.dialog != nil || m.sessions != nil || m.input.Value() != "/" {
		t.Fatal("completion click executed or inserted a command")
	}
	selected, offset := m.suggestions.Selected, m.transcript.Viewport.YOffset()
	m.handleMouse(tea.MouseWheelMsg{X: x + 2, Y: y + len(l.Rows) - 2, Button: tea.MouseWheelDown})
	if m.suggestions.Selected != selected || m.transcript.Viewport.YOffset() != offset {
		t.Fatal("completion footer wheel reached list or transcript")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if m.suggestions.Selected != min(selected+l.BodyHeight, len(m.suggestions.Items)-1) || m.transcript.Viewport.YOffset() != offset {
		t.Fatal("paging escaped completion input ownership")
	}
	l, x, y = m.suggestionLayout()
	m.handleMouse(tea.MouseClickMsg{X: x + l.Width - 4, Y: y + 1, Button: tea.MouseLeft})
	m.Update(tickMsg{})
	if m.suggestions.Pending() || m.input.Value() != "/" || !m.input.Focused() {
		t.Fatal("closing completion lost the draft or reopened it")
	}
}

func TestCompletionDragRetainsOwnershipOverToolbar(t *testing.T) {
	m := completionModel(t)
	m.input.SetValue("/")
	m.syncSuggestions()
	l, x, y := m.suggestionLayout()
	m.handleMouse(tea.MouseClickMsg{X: x + l.Width - 3, Y: y + l.BodyTop, Button: tea.MouseLeft})
	if !m.suggestions.Dragging() {
		t.Fatal("completion scrollbar did not capture mouse")
	}
	m.handleMouse(tea.MouseMotionMsg{X: m.termW - 1, Y: m.termH - 1, Button: tea.MouseLeft})
	m.handleMouse(tea.MouseReleaseMsg{X: m.termW - 1, Y: m.termH - 1, Button: tea.MouseLeft})
	if m.suggestions.Selected != len(m.suggestions.Items)-1 || m.suggestions.Dragging() || m.quit || m.input.Value() != "/" {
		t.Fatal("completion drag reached an underlying toolbar command")
	}
}

func TestCompletionFitsAllThemesAndReportsTheCurrentRunAction(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		for _, size := range [][2]int{{40, 14}, {40, 20}, {80, 24}, {120, 40}, {160, 50}} {
			m := completionModel(t)
			sizeModelTo(t, m, size[0], size[1])
			m.applyTheme(theme)
			m.input.SetValue("/compact")
			m.syncSuggestions()
			cursor := m.View().Cursor
			for _, state := range []runState{stRunning, stDone} {
				m.state = state
				l, _, _ := m.suggestionLayout()
				if len(l.Rows) > m.transcript.Viewport.Height() {
					t.Fatal("completion exceeds transcript height", theme, size)
				}
				view := m.View()
				lines := strings.Split(view.Content, "\n")
				if len(lines) != m.termH || view.Cursor.Y != cursor.Y {
					t.Fatal("completion moved composer or changed screen bounds", theme, size)
				}
				for _, line := range lines {
					if ansi.StringWidth(line) != m.termW {
						t.Fatal("completion changed screen width", theme, size)
					}
				}
				if l.Close {
					action := "ENTER run"
					if state == stRunning {
						action = "ENTER insert"
					}
					if !strings.Contains(ansi.Strip(strings.Join(l.Rows, "\n")), action) {
						t.Fatal("completion hint disagrees with execution", theme, size, state)
					}
				}
			}
		}
	}
}
