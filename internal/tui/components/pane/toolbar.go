package pane

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

type ToolAction struct {
	Key, CompactKey, Label, CompactLabel string
	Enabled                              bool
}

type ToolbarStyles struct {
	Key, Label, DisabledKey, DisabledLabel, Gap lipgloss.Style
}

type ToolCell struct {
	Bounds  Rect
	Enabled bool
}

type ToolbarLayout struct {
	Row   string
	Cells []ToolCell
}

// Toolbar gives each action an equal share of one row, separated by a cell.
// The application chooses which actions to offer; this layout owns their bounds.
func Toolbar(width int, actions []ToolAction, styles ToolbarStyles) ToolbarLayout {
	width = max(width, 0)
	count := min(len(actions), (width+1)/2)
	l := ToolbarLayout{}
	used := 0
	for i, action := range actions[:count] {
		contentWidth := width - (count - 1)
		start := i*contentWidth/count + i
		end := (i+1)*contentWidth/count + i
		w := end - start
		key, label := action.Key, action.Label
		if w < 14 && action.CompactKey != "" {
			key = action.CompactKey
		}
		key = popup.KeyLabel(key)
		if w < cell.Width(key)+2+cell.Width(label) && action.CompactLabel != "" {
			label = action.CompactLabel
		}
		keyStyle, labelStyle := styles.Key, styles.Label
		if !action.Enabled {
			keyStyle, labelStyle = styles.DisabledKey, styles.DisabledLabel
		}
		keyWidth := min(cell.Width(key)+2, w)
		l.Row += styles.Gap.Render(strings.Repeat(" ", start-used))
		l.Row += keyStyle.Render(cell.Line(" "+key+" ", keyWidth)) + labelStyle.Render(cell.Line(label, w-keyWidth))
		l.Cells = append(l.Cells, ToolCell{Bounds: Rect{X: start, Width: w, Height: 1}, Enabled: action.Enabled})
		used = end
	}
	l.Row += styles.Gap.Render(strings.Repeat(" ", width-used))
	return l
}

// Hit returns the action index, excluding gaps and disabled cells.
func (l ToolbarLayout) Hit(x, y int) int {
	for i, cell := range l.Cells {
		if cell.Enabled && cell.Bounds.Contains(x, y) {
			return i
		}
	}
	return -1
}
