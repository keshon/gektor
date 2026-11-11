package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/keshon/gektor/internal/config"
)

func TestSettingsToolbarDisabledApplyIsVisibleAndCannotRun(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		m := settingsModel(t)
		m.applyTheme(theme)
		m.editSettings("generation")
		m.chrome.Dialog.Form.EditSession.PersistCurrent = false
		if m.chrome.Dialog.Form.Dirty() {
			t.Fatal("unchanged settings fixture is dirty")
		}
		for i, action := range m.footerActions() {
			if action.Key != 's' {
				continue
			}
			if action.Enabled {
				t.Fatal("toolbar offers Apply for unchanged settings")
			}
			hit := m.chrome.ToolbarLayout().Cells[i].Bounds
			screen := uv.NewScreenBuffer(m.termW, 1)
			uv.NewStyledString(m.chrome.ActionBar()).Draw(screen, uv.Rect(0, 0, m.termW, 1))
			if c := screen.CellAt(hit.X, 0); c == nil || !sameColor(c.Style.Fg, themeColor(colorDisabledText)) {
				t.Fatal("disabled action uses enabled styling", theme)
			}
			before := m.chrome.Dialog
			_, cmd := m.handleMouse(tea.MouseClickMsg{X: hit.X, Y: m.termH - 1, Button: tea.MouseLeft})
			if cmd != nil || m.chrome.Dialog != before {
				t.Fatal("disabled Apply executed")
			}
		}
	}
}
