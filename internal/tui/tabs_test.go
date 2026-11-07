package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

func TestHelpTabsClickWrappedRowsAndPreserveDraft(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		for _, width := range []int{40, 80, 121} {
			m := sizeModelTo(t, testModel(), width, 30)
			m.applyTheme(theme)
			m.input.SetValue("unfinished draft")
			m.openHelp()
			for tab := range m.dialogHelpSections() {
				layout := m.layoutDialog()
				rows, w, h := m.dialogRows()
				left, top, _, _ := m.settingsArea()
				x := left + max((w-popup.ModalWidth(w))/2, 0) + 2
				y := top + max((h-len(rows)-2)/2, 0) + 1
				hit := layout.tabs.Hits[tab]
				m.dialog.offset = 2
				m.handleMouse(tea.MouseClickMsg{X: x + hit.X, Y: y + 2 + hit.Y, Button: tea.MouseLeft})
				if m.dialog == nil || m.dialog.page != tab || m.dialog.offset != 0 {
					t.Fatalf("%s/%d: Help tab %d click missed", theme, width, tab)
				}
				layout = m.layoutDialog()
				if layout.rows[len(layout.rows)-1] != "" || !strings.Contains(strings.Join(layout.tabs.Rows, "\n"), "["+m.dialogHelpSections()[tab].title+"]") {
					t.Fatal("tab lost active marker or space below strip")
				}
			}
			m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.input.Value() != "unfinished draft" || !m.input.Focused() {
				t.Fatal("Help tabs lost the conversation draft or focus")
			}
		}
	}
}

func TestWorkflowTabGapAndWheelDoNotChangePage(t *testing.T) {
	m := workflowTestModel(t)
	m.workflows.editor.Replace(workflowRecipe())
	m.workflowScreen()
	m.workflowCommand("inspector")
	tabs := m.workflowInspectorTabs()
	area := m.screenPanes(true).Side
	layout := m.panelGeometry(area.Width, 1, true, false)
	x, y := area.X+layout.Body.X, area.Y+layout.Body.Y
	m.workflows.offset = 3
	m.handleMouse(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown})
	if m.workflows.offset != 3 {
		t.Fatal("wheel over pinned tabs scrolled their page")
	}
	m.handleMouse(tea.MouseClickMsg{X: x + tabs.Hits[0].Width, Y: y, Button: tea.MouseLeft})
	if m.workflows.inspectorTab != 0 || m.workflows.offset != 3 {
		t.Fatal("tab gap changed the page")
	}
}
