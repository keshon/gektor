package graph

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

func portList(ports []Port, output bool) []Port {
	if ports != nil {
		return ports
	}
	label := "input"
	if output {
		label = "result"
	}
	return []Port{{Label: label}}
}

func (m Model) card(n Node, styles Styles, incoming, outgoing map[[2]string]bool, failed map[[2]string]bool) []string {
	normal := styles.Normal
	width := m.nodeWidth()
	inner := width - 2
	// The title uses a foreground/background pair from the same theme surface.
	border, title := normal, styles.Title.Bold(true)
	h, v, tl, tr, bl, br, jl, jr := "─", "│", "┌", "┐", "└", "┘", "├", "┤"
	if n.ID == m.Selected {
		border = styles.Focus.Bold(true)
		h, v, tl, tr, bl, br, jl, jr = "═", "║", "╔", "╗", "╚", "╝", "╠", "╣"
	}
	clean := func(s string) string {
		return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(ansi.Strip(s))
	}
	line := func(content string, style lipgloss.Style) string {
		return border.Render(v) + style.Render(cell.Line(content, inner)) + border.Render(v)
	}
	top := border.Render(tl + strings.Repeat(h, inner) + tr)
	bottom := border.Render(bl + strings.Repeat(h, inner) + br)
	heading := n.Title
	if heading == "" {
		heading = n.ID
	}
	if m.Compact {
		return []string{top, line(" "+clean(heading), title), bottom}
	}
	rule := border.Render(jl + strings.Repeat(h, inner) + jr)
	rows := []string{top, line(" "+clean(heading), title), line(" "+clean(n.Label), normal), rule}
	inputs, outputs := portList(n.Inputs, false), portList(n.Outputs, true)
	leftWidth := (inner - 2) / 2
	rightWidth := inner - 2 - leftWidth
	for i := 0; i < max(1, len(inputs), len(outputs)); i++ {
		left, right := "", ""
		lmark, rmark := v, v
		if i < len(inputs) {
			p := inputs[i]
			left = " " + clean(p.Label)
			lmark = "○"
			if p.Key == "@order" {
				lmark = "◇"
			}
			if incoming[[2]string{n.ID, p.Key}] {
				if p.Key == "@order" {
					lmark = "◆"
				} else {
					lmark = "●"
				}
			}
		}
		if i < len(outputs) {
			p := outputs[i]
			right = clean(p.Label)
			rmark = "○"
			if p.Key == "@order" {
				rmark = "◇"
			}
			if outgoing[[2]string{n.ID, p.Key}] {
				if p.Key == "@order" {
					rmark = "◆"
				} else {
					rmark = "●"
				}
			}
			if failed[[2]string{n.ID, p.Key}] {
				rmark = "!"
			}
		}
		right = strings.TrimSpace(cell.Line(right, rightWidth-1))
		right = strings.Repeat(" ", max(0, rightWidth-1-ansi.StringWidth(right))) + right + " "
		portStyle := border
		if i < len(outputs) && failed[[2]string{n.ID, outputs[i].Key}] {
			portStyle = styles.Error
		}
		rows = append(rows, border.Render(lmark)+normal.Render(cell.Line(left, leftWidth)+"  "+right)+portStyle.Render(rmark))
	}
	status := n.Detail
	statusStyle := normal
	switch status {
	case "":
		status = "○ Not run"
	case "completed":
		status = "✓ Completed"
		statusStyle = styles.Success
	case "running":
		status = "▶ Running"
		statusStyle = styles.Warning
	case "failed":
		status = "× Failed"
		statusStyle = styles.Error
	case "skipped":
		status = "− Skipped"
		statusStyle = styles.Disabled
	case "pending":
		status = "○ Pending"
	case "waiting_review", "Review before running":
		status = "? Awaiting approval"
		statusStyle = styles.Warning
	case "cancelled":
		status = "× Cancelled"
		statusStyle = styles.Warning
	case "interrupted":
		status = "! Interrupted"
		statusStyle = styles.Warning
	case "Disabled / condition false":
		statusStyle = styles.Disabled
	}
	return append(rows, rule, line(" "+clean(status), statusStyle), bottom)
}
