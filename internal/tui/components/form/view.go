package form

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/tui/components/cell"
)

type Hit struct{ Index, Y0, Y1, X0, X1 int }
type Styles struct{ Muted, Focus, Error lipgloss.Style }
type Props struct {
	Title, Hint, Error, PrimaryLabel, Choice string
	Width, Height                            int
	Destructive                              bool
	Styles                                   Styles
}

func (f Model) Rows(props Props) ([]string, []Hit) {
	w, h := props.Width, props.Height
	inner := max(min(w-6, 72), 12)
	height := f.Editor.Height()
	rows := []string{props.Styles.Focus.Render(props.Title), "", props.Styles.Muted.Render(cell.Line(props.Hint, inner)), ""}
	capacity := max((h-9-height)/3, 1)
	start := max(min(f.Index, len(f.Fields)-1)-capacity/2, 0)
	end := min(start+capacity, len(f.Fields))
	if end == len(f.Fields) {
		start = max(end-capacity, 0)
	}
	hits := []Hit{}
	for i := start; i < end; i++ {
		label := f.Fields[i].Label
		style := props.Styles.Muted
		if i == f.Index {
			label = "› " + label
			style = props.Styles.Focus
		}
		y := len(rows)
		rows = append(rows, style.Render(cell.Line(label, inner)))
		if i == f.Index && props.Choice != "" {
			rows = append(rows, cell.Line("[ "+props.Choice+" ]  Enter to choose", inner))
		} else if i == f.Index {
			rows = append(rows, strings.Split(f.Editor.View(), "\n")...)
		} else {
			rows = append(rows, cell.Line(strings.ReplaceAll(f.Fields[i].Value, "\n", " ↵ "), inner))
		}
		hits = append(hits, Hit{i, y, len(rows), 0, 0})
		rows = append(rows, "")
	}
	if len(f.Fields) > capacity {
		rows = append(rows, props.Styles.Muted.Render(fmt.Sprintf("Fields %d–%d / %d · Tab moves through all fields", start+1, end, len(f.Fields))))
	}
	if props.Error != "" {
		rows = append(rows, props.Styles.Error.Render(cell.Line(props.Error, inner)))
	} else {
		rows = append(rows, cell.Line("", inner))
	}
	action, actionHits := f.Actions(inner, len(rows), props.PrimaryLabel, true, props.Styles)
	rows = append(rows, action, props.Styles.Muted.Render(strings.Repeat("─", inner)))
	hits = append(hits, actionHits...)
	hint := "↑↓/Tab fields · Ctrl+↑↓ multiline fields · Ctrl+S apply · Esc cancel"
	if props.Destructive {
		hint = "Tab selects · Enter confirms selection · Esc cancel"
	}
	rows = append(rows, props.Styles.Muted.Render(cell.Line(hint, inner)))
	return rows, hits
}
func (f Model) Actions(width, y int, primary string, enabled bool, styles Styles) (string, []Hit) {
	cancel := "[ Cancel ]"
	if width < 20 {
		cancel = "[Esc]"
	}
	budget := max(width-lipgloss.Width(cancel)-2, 3)
	button := "[ " + primary + " ]"
	if lipgloss.Width(button) > budget {
		if budget >= 5 {
			button = "[" + ansi.Truncate(primary, budget-2, "…") + "]"
		} else {
			button = ansi.Truncate(primary, budget, "…")
		}
	}
	padding := max(width-lipgloss.Width(button)-lipgloss.Width(cancel)-2, 0)
	row := strings.Repeat(" ", padding)
	hits := []Hit{}
	for i, text := range []string{button, cancel} {
		if i > 0 {
			row += "  "
		}
		start := lipgloss.Width(row)
		style := styles.Muted
		if f.Index == len(f.Fields)+i && (i != 0 || enabled) {
			style = styles.Focus
		}
		row += style.Render(text)
		hits = append(hits, Hit{len(f.Fields) + i, y, y + 1, start, lipgloss.Width(row)})
	}
	return cell.Line(row, width), hits
}
