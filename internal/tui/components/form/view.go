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

	// Hooks for a form that needs more than plain fields. Every one is
	// optional: a nil or zero value means the default, so a simple form
	// draws exactly what it always did. They exist because the settings
	// editor forked this renderer to get them, and two renderers of the
	// same skeleton is how a form stops looking like the rest of the app.
	//
	// Visible hides fields - the settings editor keeps a rarely used field
	// behind a toggle rather than showing it greyed out.
	Visible func(index int) bool
	// Label rewrites a field's name, for one whose stored key reads worse
	// than the thing it describes.
	Label func(Field) string
	// Display renders a field's value as a row - shortened, annotated with
	// what pressing it does - and reports whether that field still opens an
	// editor. A field that opens a chooser is not editable in place, so it
	// stays a value even while focused. Nil means the plain behaviour: the
	// stored value, and an editor on the focused field.
	Display func(field Field, current string) (string, bool)
	// Note is the line above the buttons when there is nothing wrong.
	Note string
	// Footer replaces the key hints at the bottom.
	Footer string
	// DisablePrimary greys the primary action out. Inverted so that the zero
	// value keeps it enabled: a form that has nothing to save should not
	// offer a button that saves nothing.
	DisablePrimary bool
}

func (f Model) Rows(props Props) ([]string, []Hit) {
	w, h := props.Width, props.Height
	inner := max(min(w-6, 72), 12)
	height := f.Editor.Height()
	rows := []string{props.Styles.Focus.Render(props.Title), "", props.Styles.Muted.Render(cell.Line(props.Hint, inner)), ""}

	// Window over the fields that are actually showing, not over all of
	// them: a hidden field must not take a row or a scroll step.
	var indexes []int
	for i := range f.Fields {
		if props.Visible == nil || props.Visible(i) {
			indexes = append(indexes, i)
		}
	}
	position := 0
	for p, i := range indexes {
		if i == f.Index {
			position = p
		}
	}
	capacity := max((h-9-height)/3, 1)
	start := max(min(position-capacity/2, len(indexes)-capacity), 0)
	end := min(start+capacity, len(indexes))

	hits := []Hit{}
	for _, i := range indexes[start:end] {
		field := f.Fields[i]
		label := field.Label
		if props.Label != nil {
			if custom := props.Label(field); custom != "" {
				label = custom
			}
		}
		style := props.Styles.Muted
		if i == f.Index {
			label = "› " + label
			style = props.Styles.Focus
		}
		y := len(rows)
		rows = append(rows, style.Render(cell.Line(label, inner)))
		value := strings.ReplaceAll(field.Value, "\n", " ↵ ")
		if i == f.Index {
			value = strings.ReplaceAll(f.Editor.Value(), "\n", " ↵ ")
		}
		editable := true
		if props.Display != nil {
			value, editable = props.Display(field, value)
		}
		switch {
		case i == f.Index && props.Choice != "":
			rows = append(rows, cell.Line("[ "+props.Choice+" ]  Enter to choose", inner))
		case i == f.Index && editable:
			rows = append(rows, strings.Split(f.Editor.View(), "\n")...)
		default:
			rows = append(rows, cell.Line(value, inner))
		}
		hits = append(hits, Hit{i, y, len(rows), 0, 0})
		rows = append(rows, "")
	}
	if len(indexes) > capacity {
		// Capped like every other row here. It was not, so a form with
		// more fields than fit pushed its own frame wider than the pane and
		// clipped its own right-hand border.
		rows = append(rows, props.Styles.Muted.Render(cell.Line(fmt.Sprintf("Fields %d–%d / %d · Tab moves through all fields", start+1, end, len(indexes)), inner)))
	}
	if props.Error != "" {
		rows = append(rows, props.Styles.Error.Render(cell.Line(props.Error, inner)))
	} else {
		rows = append(rows, cell.Line(props.Note, inner))
	}
	action, actionHits := f.Actions(inner, len(rows), props.PrimaryLabel, !props.DisablePrimary, props.Styles)
	rows = append(rows, action, props.Styles.Muted.Render(strings.Repeat("─", inner)))
	hits = append(hits, actionHits...)
	footer := props.Footer
	if footer == "" {
		footer = "↑↓/Tab fields · Ctrl+↑↓ multiline fields · Ctrl+S apply · Esc cancel"
		if props.Destructive {
			footer = "Tab selects · Enter confirms selection · Esc cancel"
		}
	}
	rows = append(rows, props.Styles.Muted.Render(cell.Line(footer, inner)))
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
