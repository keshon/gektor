package settings

import (
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"

	"charm.land/lipgloss/v2"
	"github.com/keshon/tars/internal/tui/components/cell"
	"github.com/keshon/tars/internal/tui/components/form"
)

type Styles struct{ Muted, Focus, Error, Selected lipgloss.Style }
type Props struct {
	Width, Height int
	Left, Top     int
	Title         string
	Styles        Styles
	SearchStyles  textinput.Styles
}

func (p Props) formStyles() form.Styles {
	return form.Styles{Muted: p.Styles.Muted, Focus: p.Styles.Focus, Error: p.Styles.Error}
}
func (f *Model) Rows(props Props) ([]string, []form.Hit, int, int) {
	if f.Picker != nil {
		return f.PickerRows(props)
	}
	if f.Workspace == nil {
		return f.PlainRows(props)
	}
	w, h := props.Width, props.Height
	rail := 0
	if w >= 76 {
		rail = 17
	}
	inner := max(w-6-rail, 12)
	category := ""
	if f.Workspace.Section == "application" {
		category = "Application"
	}
	for _, c := range categories {
		if c.ID == f.Workspace.Section {
			category = c.Label
		}
	}
	hint := Hint(f.Workspace.Section)
	rows := []string{props.Styles.Focus.Render("Settings / " + category), props.Styles.Muted.Render(cell.Line(hint, inner)), ""}
	hits := []form.Hit{}
	visible := []int{}
	for i := range f.Fields {
		if f.FieldVisible(i) {
			visible = append(visible, i)
		}
	}
	capacity := max((h-10)/3, 1)
	position := 0
	for i, v := range visible {
		if v == f.Index {
			position = i
		}
	}
	start := max(min(position-capacity/2, len(visible)-capacity), 0)
	end := min(start+capacity, len(visible))
	for _, i := range visible[start:end] {
		field := f.Fields[i]
		label := field.Label
		switch field.Key {
		case "endpoint":
			label = "Server address"
		case "kind":
			label = "Protocol"
		}
		style := props.Styles.Muted
		if i == f.Index && !f.Workspace.Nav {
			style = props.Styles.Focus
			label = "› " + label
		}
		y := len(rows)
		rows = append(rows, style.Render(cell.Line(label, inner)))
		value := field.Value
		if i == f.Index {
			value = f.Editor.Value()
		}
		if PickerFiles(field.Key) {
			if value == "" {
				value = "Not selected"
			} else {
				value = filepath.Base(value)
			}
			value += "  [ Choose… ]"
		}
		if field.Key == "model" {
			value = value + "  [ Discover… ]"
		}
		if len(f.Options(field.Key)) > 0 {
			value = "[ " + ChoiceLabel(field.Key, value) + " ▾ ]"
		}
		if i == f.Index && !PickerControl(field.Key) && len(f.Options(field.Key)) == 0 {
			rows = append(rows, strings.Split(f.Editor.View(), "\n")...)
		} else {
			rows = append(rows, cell.Line(strings.ReplaceAll(value, "\n", " ↵ "), inner))
		}
		hits = append(hits, form.Hit{Index: i, Y0: y, Y1: len(rows), X0: 0, X1: 0})
		rows = append(rows, "")
	}
	// Actions stay at the bottom; a small terminal shows fewer controls.
	for len(rows) < max(h-7, 3) {
		rows = append(rows, "")
	}
	note := f.Error
	if note == "" {
		note = "No changes · F4 Advanced · F6 Categories"
		if f.Dirty() {
			note = "Apply saves a custom profile · F4 Advanced · F6 Categories"
		}
		if f.Workspace.Section == "profiles" {
			note = "Ctrl+D saves a copy · Apply uses the loaded profile in this session"
		}
	}
	rows = append(rows, props.Styles.Muted.Render(cell.Line(note, inner)))
	buttonRow, actionHits := f.Actions(inner, len(rows), "Apply", f.Dirty(), props.formStyles())
	hits = append(hits, actionHits...)
	rows = append(rows, buttonRow, props.Styles.Muted.Render(strings.Repeat("─", inner)))
	rows = append(rows, props.Styles.Muted.Render(cell.Line("Tab controls · Ctrl+S apply · Esc cancel", inner)))
	if rail > 0 {
		for i := range rows {
			label := ""
			if categories := f.AvailableCategories(); i < len(categories) {
				c := categories[i]
				label = "  " + c.Label
				if c.ID == f.Workspace.Section {
					label = "› " + c.Label
					style := props.Styles.Selected
					if f.Workspace.Nav {
						style = props.Styles.Focus
					}
					label = style.Render(label)
				}
			}
			rows[i] = cell.Line(label, rail-2) + props.Styles.Muted.Render("│ ") + cell.Line(rows[i], inner)
		}
	}
	return rows, hits, w, h
}

func (f *Model) PlainRows(props Props) ([]string, []form.Hit, int, int) {
	w, h := props.Width, props.Height
	choice := ""
	if f.Index < len(f.Fields) && len(f.Options(f.Fields[f.Index].Key)) > 0 {
		choice = ChoiceLabel(f.Fields[f.Index].Key, f.Editor.Value())
	}
	rows, hits := f.Model.Rows(form.Props{Title: props.Title, Hint: f.Hint, Error: f.Error, PrimaryLabel: f.ApplyLabel, Width: w, Height: h, Destructive: f.Destructive, Choice: choice, Styles: props.formStyles()})
	return rows, hits, w, h
}

func (f *Model) View(props Props) string {
	rows, _, w, h := f.Rows(props)
	width := max(props.Width, 0)
	rail := f.Picker == nil && w >= 76
	rule := strings.Repeat("─", width)
	if rail {
		rule = strings.Repeat("─", 16) + "┬" + strings.Repeat("─", max(width-17, 0))
	}
	out := []string{props.Styles.Muted.Render("╭" + rule + "╮")}
	for i := 0; i < max(h-2, 0); i++ {
		row := ""
		if i < len(rows) {
			row = rows[i]
		} else if rail {
			row = strings.Repeat(" ", 15) + props.Styles.Muted.Render("│ ")
		}
		out = append(out, props.Styles.Muted.Render("│ ")+cell.Line(row, max(width-2, 0))+props.Styles.Muted.Render(" │"))
	}
	if rail {
		rule = strings.Replace(rule, "┬", "┴", 1)
	}
	out = append(out, props.Styles.Muted.Render("╰"+rule+"╯"))
	return strings.Join(out, "\n")
}
