package screen

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/cell"
)

// Compose adds application chrome to a feature's full-terminal content and cursor.
// Dialog coordinates match input hit testing through the same Props.Area.
func (m *Controls) Compose(view tea.View) tea.View {
	rows := strings.Split(cell.Frame(view.Content, m.Props.Width, m.Props.Height), "\n")
	if len(rows) == 0 {
		return view
	}
	rows[0] = cell.Line(m.MenuLine(), m.Props.Width)
	if m.ActionBarVisible() {
		rows[len(rows)-1] = cell.Line(m.ActionBar(), m.Props.Width)
	}
	content := strings.Join(rows, "\n")
	if m.Dialog != nil {
		content = m.overlayContent(content)
		view.Cursor = nil
	}
	if m.Header.Menu != "" {
		content = m.OverlayHeader(content)
		view.Cursor = nil
	}
	view.Content = Paint(content)
	return view
}

func (m *Controls) overlayContent(content string) string {
	area := m.Props.Area
	if !m.Props.Ready {
		area.X, area.Y, area.Width, area.Height = 0, 0, m.Props.Width, m.Props.Height
	}
	rows := strings.Split(content, "\n")
	body := make([]string, max(area.Height, 0))
	for i := range body {
		if y := area.Y + i; y >= 0 && y < len(rows) {
			body[i] = cell.Line(ansi.Cut(rows[y], area.X, area.X+area.Width), area.Width)
		}
	}
	overlay := strings.Split(m.OverlayDialog(strings.Join(body, "\n")), "\n")
	for i, line := range overlay {
		y := area.Y + i
		if y < 0 || y >= len(rows) || i >= area.Height {
			continue
		}
		rows[y] = cell.Line(ansi.Cut(rows[y], 0, area.X), area.X) + cell.Line(line, area.Width) + ansi.Cut(rows[y], area.X+area.Width, m.Props.Width)
	}
	return strings.Join(rows, "\n")
}
