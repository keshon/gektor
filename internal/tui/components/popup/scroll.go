package popup

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

// ScrollTrack uses terminal coordinates and source-row offsets, like ScrollRows.
type ScrollTrack struct {
	X, Y, Height, Total, Offset int
}

// ScrollDrag retains a thumb grab until release, even outside the track.
// A changed track cancels the grab before applying another movement.
type ScrollDrag struct {
	Active bool
	grab   int
	row    int
	track  ScrollTrack
}

func (d *ScrollDrag) Mouse(msg tea.MouseMsg, track ScrollTrack) (int, bool) {
	mouse := msg.Mouse()
	row := mouse.Y - track.Y
	offset := min(max(track.Offset, 0), max(track.Total-track.Height, 0))
	geometry := track
	geometry.Offset = 0

	if d.Active {
		if geometry != d.track || track.Height <= 0 || track.Total <= track.Height {
			*d = ScrollDrag{}
			return offset, true
		}

		switch msg.(type) {
		case tea.MouseMotionMsg, tea.MouseReleaseMsg:
			if row != d.row {
				offset = ScrollOffset(row, track.Height, track.Total, d.grab)
				d.row = row
			}
			if _, released := msg.(tea.MouseReleaseMsg); released {
				*d = ScrollDrag{}
			}
		}

		return offset, true
	}

	_, clicked := msg.(tea.MouseClickMsg)
	if !clicked || mouse.Button != tea.MouseLeft || mouse.X != track.X || row < 0 || row >= track.Height || track.Total <= track.Height {
		return offset, false
	}

	top, size := ScrollThumb(track.Height, track.Total, offset)
	switch {
	case row < top:
		offset = max(offset-track.Height, 0)
	case row >= top+size:
		offset = min(offset+track.Height, track.Total-track.Height)
	default:
		*d = ScrollDrag{Active: true, grab: row - top, row: row, track: geometry}
	}

	return offset, true
}

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
	// A one-cell track has no thumb travel; dragging beyond it selects an end.
	if height == 1 {
		if row > grab {
			return max(total-height, 0)
		}
		return 0
	}

	_, size := ScrollThumb(height, total, 0)
	return min(max(row-grab, 0), height-size) * max(total-height, 0) / max(height-size, 1)
}
