package tui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestWorkflowCardTitleAndSelectionInBothThemes(t *testing.T) {
	previous := selectedPalette.Load()
	defer selectedPalette.Store(previous)
	for _, theme := range []string{"light", "dark"} {
		m := workflowTestModel(t)
		m.applyTheme(theme)
		d := workflowRecipe()
		next := d.Steps[0]
		next.ID = "other"
		d.Steps = append(d.Steps, next)
		m.workflows.editor.Replace(d)
		view := m.View().Content
		screen := uv.NewScreenBuffer(m.termW, m.termH)
		uv.NewStyledString(view).Draw(screen, uv.Rect(0, 0, m.termW, m.termH))
		rows := strings.Split(ansi.Strip(view), "\n")
		for _, n := range m.workflows.graph.Nodes {
			title := screen.CellAt(n.X+3, paneTop+n.Y+1)
			if title == nil || !sameColor(title.Style.Fg, themeColor(colorSelectedText)) || !sameColor(title.Style.Bg, themeColor(colorSelectedBackground)) {
				t.Fatalf("%s: unreadable title for %s: %+v", theme, n.ID, title)
			}
			border := screen.CellAt(n.X+1, paneTop+n.Y)
			if border == nil || !sameColor(border.Style.Fg, themeColor(colorPopupText)) {
				t.Fatalf("%s: frame uses title foreground on canvas", theme)
			}
			corner := "┌"
			if n.ID == m.workflows.graph.Selected {
				corner = "╔"
			}
			if ansi.Cut(rows[paneTop+n.Y], n.X+1, n.X+2) != corner {
				t.Fatalf("%s: selected frame not distinct", theme)
			}
		}
	}
}
