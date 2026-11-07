package tui

import (
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
