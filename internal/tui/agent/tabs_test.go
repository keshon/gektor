package agent

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
				layout := m.testControls().LayoutDialog()
				rows, w, h := m.testControls().DialogRows()
				left, top, _, _ := m.dialogArea()
				x := left + max((w-popup.ModalWidth(w))/2, 0) + 2
				y := top + max((h-len(rows)-2)/2, 0) + 1
				hit := layout.Tabs.Hits[tab]
				m.chrome.Dialog.Offset = 2
				m.handleMouse(tea.MouseClickMsg{X: x + hit.X, Y: y + 2 + hit.Y, Button: tea.MouseLeft})
				if m.chrome.Dialog == nil || m.chrome.Dialog.Page != tab || m.chrome.Dialog.Offset != 0 {
					t.Fatalf("%s/%d: Help tab %d click missed", theme, width, tab)
				}
				layout = m.testControls().LayoutDialog()
				if layout.Rows[len(layout.Rows)-1] != "" || !strings.Contains(strings.Join(layout.Tabs.Rows, "\n"), "["+m.dialogHelpSections()[tab].title+"]") {
					t.Fatal("tab lost active marker or space below strip")
				}
			}
			m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.input.Value() != "unfinished draft" || !m.input.Focused() {
				t.Fatal("Help tabs lost the conversation draft or focus")
			}
		}
	}
}
