package popup

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

// ScrollRows reserves a one-cell gutter before the proportional track and thumb.
// offset and total use source rows; len(rows) is the visible window.
func ScrollRows(rows []string, width, offset, total int, style lipgloss.Style) []string {
	if total <= len(rows) || len(rows) == 0 {
		return rows
	}
	gutter := ""
	if width > 1 {
		gutter = " "
	}
	contentWidth := max(width-1-len(gutter), 0)
	top, size := ScrollThumb(len(rows), total, offset)
	out := make([]string, len(rows))
	for i, row := range rows {
		glyph := "│"
		if i >= top && i < top+size {
			glyph = "█"
		}
		out[i] = cell.Line(ansi.Truncate(row, contentWidth, ""), contentWidth) + style.Render(gutter+glyph)
	}
	return out
}

func ScrollThumb(height, total, offset int) (top, size int) {
	size = min(max(height*height/max(total, 1), 1), height)
	top = min(max(offset, 0), max(total-height, 0)) * (height - size) / max(total-height, 1)
	return
}

func ScrollOffset(row, height, total, grab int) int {
	_, size := ScrollThumb(height, total, 0)
	return min(max(row-grab, 0), height-size) * max(total-height, 0) / max(height-size, 1)
}
