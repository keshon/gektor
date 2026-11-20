package agent

import (
	tea "charm.land/bubbletea/v2"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func testFormSnapshot(m *Model) fieldform.Snapshot {
	s, _ := m.chrome.FormSnapshot(m.chrome.DialogID())
	return s
}
func testFormOpen(m *Model) bool                { return m.chrome.DialogInfo().Kind == screen.FormDialog }
func testTableOpen(m *Model) bool               { return m.chrome.DialogInfo().Kind == screen.TableDialog }
func testFormValues(m *Model) map[string]string { return testFormSnapshot(m).Values() }
func testFormValue(m *Model, text string) {
	m.chrome.FormKey(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	m.chrome.UpdateFormInput(tea.PasteMsg{Content: text})
}
func testHelpPage(m *Model, page int) {
	for range len(helpSections()) + 1 {
		if m.chrome.DialogInfo().Page == page {
			return
		}
		m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
	}
	panic("help page is unreachable")
}
