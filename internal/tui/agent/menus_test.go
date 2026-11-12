package agent

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestMenuMouseIgnoresOutsideWheelFrameAndDisabledRows(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		m := sizeModelTo(t, testModel(), 120, 30)

		m.applyTheme(theme)
		m.chrome.Header = screen.MenuState{Global: true, Focused: true, Menu: "session"}
		m.chrome.Header.Row = m.testControls().HeaderActionRow(0, 1)
		l := m.testControls().HeaderMenuLayout()
		before := m.chrome.Header.Row
		m.headerMouse(tea.MouseWheelMsg{X: 119, Y: 27, Button: tea.MouseWheelDown})
		if m.chrome.Header.Row != before {
			t.Fatal("outside wheel changed menu selection", theme)
		}
		m.headerMouse(tea.MouseClickMsg{X: l.X, Y: l.Y + 1, Button: tea.MouseLeft})
		if m.chrome.Dialog != nil || m.chrome.Header.Menu != "session" {
			t.Fatal("menu frame activated a command", theme)
		}
		// A disabled command uses disabled styling even if stale selection points at it.
		m.chrome.Header.Menu = "session"
		m.state = stRunning
		for i, item := range m.testControls().HeaderMenuItems() {
			if item.Enabled || item.Info || item.Separator {
				continue
			}
			m.chrome.Header.Row = i
			l = m.testControls().HeaderMenuLayout()
			y := l.Y + 1 + i - l.Offset
			screen := uv.NewScreenBuffer(m.termW, m.termH)
			uv.NewStyledString(m.testControls().OverlayHeader(m.testView().Content)).Draw(screen, uv.Rect(0, 0, m.termW, m.termH))
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

func TestNarrowMenuOverflowReachesEveryMenu(t *testing.T) {
	for _, width := range []int{24, 40, 80, 120} {
		m := sizeModelTo(t, testModel(), width, 24)

		reached := map[string]bool{}
		for _, cell := range m.testControls().MenuCells() {
			if cell.End > width {
				t.Fatal("menu outside terminal", width, cell)
			}
			if cell.Action.Name != "overflow" {
				reached[cell.Action.Name] = true
				continue
			}
			m.chrome.Header = screen.MenuState{Global: true, Focused: true, Menu: "overflow"}
			items := m.testControls().HeaderItems()
			for i, item := range items {
				m.chrome.Header.Menu, m.chrome.Header.Row = "overflow", i
				m.headerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
				target := strings.TrimPrefix(item.Command, "menu:")
				if m.chrome.Header.Menu != target || !m.chrome.Header.Global {
					t.Fatal("overflow target did not open", target)
				}
				layout := m.testControls().HeaderMenuLayout()
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

func TestCompactMenuRequiresExplicitConfirmation(t *testing.T) {
	m := settingsModel(t)
	m.activateMenuAction("compact-session")
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil || m.chrome.Dialog.Form.Index != 1 {
		t.Fatal("compaction did not start on Cancel")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.chrome.Dialog != nil {
		t.Fatal("compaction could not be cancelled")
	}
}
