package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
)

func TestFormsShareRightAlignedActionRowAndDivider(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		for _, workspace := range []bool{false, true} {
			m := settingsModel(t)
			m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
			if workspace {
				m.editSettings("generation")
			} else {
				m.openForm("Delete Identity", "Removes the saved Identity.", "Delete", nil, func(map[string]string) error { return nil })
			}
			rows, hits, _, _ := m.formRows()
			buttons := hits[len(hits)-2:]
			if buttons[0].Y0 != buttons[1].Y0 || buttons[0].X1 >= buttons[1].X0 {
				t.Fatal("buttons not on one row", buttons)
			}
			y := buttons[0].Y0
			line := ansi.Strip(rows[y])
			// Settings include the category rail; action coordinates exclude it.
			if !strings.HasSuffix(line, "[ Cancel ]") {
				t.Fatal("actions not right aligned", line)
			}
			if y < 2 || !strings.Contains(ansi.Strip(rows[y-2]), "────") || y != len(rows)-1 {
				t.Fatal("actions must sit in the footer beneath its divider")
			}
		}
	}
}

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

func TestFormFooterMatchesActionsAndViewIsPure(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("generation")
	before := m.chrome.Dialog.Form.Editor.Value()
	cursor := m.chrome.Dialog.Form.Editor.Column()
	first := m.View().Content
	second := m.View().Content
	if first != second || m.chrome.Dialog.Form.Editor.Value() != before || m.chrome.Dialog.Form.Editor.Column() != cursor {
		t.Fatal("render mutated form")
	}
	for _, action := range m.footerActions() {
		if action.Name == "F1" {
			t.Fatal("footer advertises unrelated help action")
		}
	}
	for _, cell := range m.chrome.FooterCells() {
		if cell.Action.Name == "Esc" {
			m.Update(tea.MouseClickMsg{X: cell.Start, Y: m.termH - 1, Button: tea.MouseLeft})
			break
		}
	}
	if m.chrome.Dialog != nil {
		t.Fatal("footer cancel click failed")
	}
}
