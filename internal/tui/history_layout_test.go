package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/llm"
)

func TestTaskModalsKeepHistoryBehindTheirFrame(t *testing.T) {
	for _, kind := range []string{"info", "rename", "delete", "settings"} {
		t.Run(kind, func(t *testing.T) {
			m := settingsModelWithHistory(t, nil, []llm.Message{{Role: llm.RoleAssistant, Content: strings.Repeat("VISIBLE HISTORY\n", 80)}})
			m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
			m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
			before := ansi.Strip(m.View().Content)
			switch kind {
			case "info":
				m.chrome.Open("About", []string{"GEKTOR"})
			case "settings":
				m.editSettings("generation")
			default:
				m.openForm(kind+" session", "Confirm the change.", "Apply", nil, func(map[string]string) error { return nil })
			}
			view := ansi.Strip(m.View().Content)
			if !strings.Contains(view, "VISIBLE HISTORY") || !strings.Contains(view, "╭") {
				t.Fatal("modal cleared the document behind it")
			}
			m.closeDialog()
			if ansi.Strip(m.View().Content) != before {
				t.Fatal("closing modal moved the reading position")
			}
		})
	}
}
