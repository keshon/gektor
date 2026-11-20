package markdown

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	extast "github.com/yuin/goldmark/extension/ast"
)

func (r renderer) table(table *extast.Table, width int) []string {
	var cells [][]string
	widths := make([]int, len(table.Alignments))
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		var values []string
		style := r.styles.Text
		if row == table.FirstChild() {
			style = style.Bold(true)
		}
		for column := row.FirstChild(); column != nil; column = column.NextSibling() {
			value := r.inlines(column, style)
			index := len(values)
			widths[index] = max(widths[index], ansi.StringWidth(value), 2)
			values = append(values, value)
		}
		cells = append(cells, values)
	}
	if len(cells) == 0 {
		return nil
	}

	// Very narrow tables become labelled records, keeping every cell visible.
	available := width - 3*len(widths) - 1
	if available < 3*len(widths) {
		var rows []string
		for _, values := range cells[1:] {
			if len(rows) > 0 {
				rows = append(rows, "")
			}
			for i, value := range values {
				rows = append(rows, wrap(cells[0][i]+": "+value, width)...)
			}
		}
		if len(rows) == 0 {
			return wrap(strings.Join(cells[0], " / "), width)
		}
		return rows
	}
	for i := range widths {
		widths[i] = min(widths[i], available)
	}
	for {
		total, largest := 0, 0
		for i, size := range widths {
			total += size
			if size > widths[largest] {
				largest = i
			}
		}
		if total <= available {
			break
		}
		widths[largest]--
	}

	separator := r.styles.Muted.Render("│")
	var rows []string
	for rowIndex, values := range cells {
		wrapped := make([][]string, len(widths))
		height := 1
		for i, value := range values {
			wrapped[i] = wrap(value, widths[i])
			height = max(height, len(wrapped[i]))
		}
		for line := range height {
			var out strings.Builder
			out.WriteString(separator)
			for i, fragments := range wrapped {
				value := ""
				if line < len(fragments) {
					value = fragments[line]
				}
				space := max(widths[i]-ansi.StringWidth(value), 0)
				left := 0
				if table.Alignments[i] == extast.AlignRight {
					left = space
				} else if table.Alignments[i] == extast.AlignCenter {
					left = space / 2
				}
				out.WriteString(" " + strings.Repeat(" ", left) + value + strings.Repeat(" ", space-left) + " " + separator)
			}
			rows = append(rows, out.String())
		}
		if rowIndex == 0 {
			parts := make([]string, len(widths))
			for i, size := range widths {
				parts[i] = strings.Repeat("─", size+2)
			}
			rows = append(rows, r.styles.Muted.Render("├"+strings.Join(parts, "┼")+"┤"))
		}
	}
	return rows
}
