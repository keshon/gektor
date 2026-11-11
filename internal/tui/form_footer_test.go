package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
