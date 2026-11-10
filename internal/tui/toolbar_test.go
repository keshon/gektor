package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/keshon/gektor/internal/config"
)

func TestSessionToolbarClickReturnsToExistingDraft(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("unfinished message")
	m.openSessions()
	for i, action := range m.footerActions() {
		if action.key == 'p' {
			hit := m.toolbarLayout().Cells[i].Bounds
			m.handleMouse(tea.MouseClickMsg{X: hit.X, Y: m.termH - 1, Button: tea.MouseLeft})
			if m.sessions != nil || m.input.Value() != "unfinished message" || !m.input.Focused() {
				t.Fatal("session toolbar ignored its action or lost the draft")
			}
			return
		}
	}
	t.Fatal("session search toolbar action missing")
}

func TestFormToolbarUsesTheFormsPrimaryAction(t *testing.T) {
	m := sizeModel(t, testModel())
	m.openForm("Open recipe", "", "Open", nil, func(map[string]string) error { return nil })
	for _, action := range m.footerActions() {
		if action.key == 's' {
			if action.label != "Open" || !action.enabled {
				t.Fatal("toolbar disagrees with the form action")
			}
			return
		}
	}
	t.Fatal("form toolbar primary action missing")
}

func TestSettingsToolbarDisabledApplyIsVisibleAndCannotRun(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		m := settingsModel(t)
		m.applyTheme(theme)
		m.editSettings("generation")
		m.dialog.form.EditSession.PersistCurrent = false
		if m.dialog.form.Dirty() {
			t.Fatal("unchanged settings fixture is dirty")
		}
		for i, action := range m.footerActions() {
			if action.key != 's' {
				continue
			}
			if action.enabled {
				t.Fatal("toolbar offers Apply for unchanged settings")
			}
			hit := m.toolbarLayout().Cells[i].Bounds
			screen := uv.NewScreenBuffer(m.termW, 1)
			uv.NewStyledString(m.actionBar()).Draw(screen, uv.Rect(0, 0, m.termW, 1))
			if c := screen.CellAt(hit.X, 0); c == nil || !sameColor(c.Style.Fg, themeColor(colorDisabledText)) {
				t.Fatal("disabled action uses enabled styling", theme)
			}
			before := m.dialog
			_, cmd := m.handleMouse(tea.MouseClickMsg{X: hit.X, Y: m.termH - 1, Button: tea.MouseLeft})
			if cmd != nil || m.dialog != before {
				t.Fatal("disabled Apply executed")
			}
		}
	}
}
