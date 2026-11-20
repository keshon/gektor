package agent

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestHelpPagesFitAndRemainReachable(t *testing.T) {
	for _, width := range []int{40, 80, 140} {
		for _, height := range []int{18, 30} {
			m := testModel()
			m.testUpdate(tea.WindowSizeMsg{Width: width, Height: height})
			m.input.SetValue("draft\nsecond line")
			m.openHelp()
			for page, section := range helpSections() {
				if m.chrome.DialogInfo().Page != page {
					t.Fatalf("page = %d, want %d", m.chrome.DialogInfo().Page, page)
				}
				for _, offset := range []int{0, 1000} {
					m.chrome.ScrollDialog(offset - m.chrome.DialogInfo().Offset)
					view := m.testControls().DialogView()
					if lipgloss.Height(view) > m.transcript.Viewport.Height() {
						t.Fatalf("%dx%d: help exceeds viewport height", width, height)
					}
					for _, line := range strings.Split(view, "\n") {
						if lipgloss.Width(line) > m.transcript.Viewport.Width() {
							t.Fatalf("%dx%d: help exceeds viewport width: %q", width, height, line)
						}
					}
					if offset > 0 {
						last := section.rows[len(section.rows)-1][1]
						words := strings.Fields(last)
						if !strings.Contains(view, words[len(words)-1]) {
							t.Fatalf("%dx%d: last action unreachable on %s", width, height, section.title+"\n"+view)
						}
					}
				}
				m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
				if m.chrome.DialogInfo().Offset != 0 {
					t.Fatal("category change must reset scrolling")
				}
			}
			if m.chrome.DialogInfo().Page != 0 {
				t.Fatal("categories must wrap")
			}
			m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
			if m.chrome.DialogInfo().Page != len(helpSections())-1 {
				t.Fatal("Shift+Tab must go backwards")
			}
			m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEscape})
			if m.chrome.DialogInfo().Kind != 0 || m.input.Value() != "draft\nsecond line" {
				t.Fatal("help changed the draft")
			}
		}
	}
}
