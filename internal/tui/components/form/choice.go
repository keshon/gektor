package form

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

// Option separates the stored value from its label.
type Option struct{ Value, Label string }

// ChoiceRows renders radios, or a checkbox for a boolean pair. Drawing and
// mouse targets share the same wrapped layout in terminal cells.
func ChoiceRows(options []Option, value string, width, maxRows int, focused bool, styles Styles) ([]string, []Hit) {
	if len(options) == 2 && ((options[0].Value == "true" && options[1].Value == "false") || (options[1].Value == "true" && options[0].Value == "false")) {
		next, label := 1, "[ ] Off"
		for i, option := range options {
			if option.Value != value {
				next = i + 1
			}
		}
		if value == "true" {
			label = "[x] On"
		}
		style := styles.Normal
		if focused {
			style = styles.Focus
		}
		return []string{style.Render(cell.Line(label, width))}, []Hit{{Option: next, Y1: 1, X1: min(cell.Width(label), width)}}
	}
	rows := []string{}
	hits := []Hit{}
	row, x := "", 0
	for i, option := range options {
		mark := "( ) "
		if option.Value == value {
			mark = "(*) "
		}
		label := ansi.Truncate(mark+option.Label, width, "…")
		length := cell.Width(label)
		if x > 0 && x+2+length > width {
			rows = append(rows, cell.Line(row, width))
			row, x = "", 0
		}
		if x > 0 {
			row += strings.Repeat(" ", 2)
			x += 2
		}
		style := styles.Normal
		if focused && option.Value == value {
			style = styles.Focus
		}
		row += style.Render(label)
		hits = append(hits, Hit{Option: i + 1, Y0: len(rows), Y1: len(rows) + 1, X0: x, X1: x + length})
		x += length
	}
	rows = append(rows, cell.Line(row, width))
	if len(rows) > maxRows {
		selected := 0
		for i, option := range options {
			if option.Value == value {
				selected = i
			}
		}
		label := "‹ " + cell.Line(options[selected].Label, max(width-4, 1)) + " ›"
		style := styles.Normal
		if focused {
			style = styles.Focus
		}
		return []string{style.Render(cell.Line(label, width))}, []Hit{
			{Option: (selected+len(options)-1)%len(options) + 1, Y1: 1, X1: 2},
			{Option: (selected+1)%len(options) + 1, Y1: 1, X0: 2, X1: width},
		}
	}
	return rows, hits
}
