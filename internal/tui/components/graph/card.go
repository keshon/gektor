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

func (m Model) card(n Node, normal, selected lipgloss.Style, incoming, outgoing map[[2]string]bool, failed map[[2]string]bool) []string {
	width := m.nodeWidth()
	inner := width - 2
	// The title uses a foreground/background pair from the same theme surface.
	border, title := normal, selected.Bold(true)
	h, v, tl, tr, bl, br, jl, jr := "─", "│", "┌", "┐", "└", "┘", "├", "┤"
	if n.ID == m.Selected {
		border = normal.Bold(true)
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
		rows = append(rows, border.Render(lmark)+normal.Render(cell.Line(left, leftWidth)+"  "+right)+border.Render(rmark))
	}
	status := n.Detail
	switch status {
	case "":
		status = "○ Not run"
	case "completed":
		status = "✓ Completed"
	case "running":
		status = "▶ Running"
	case "failed":
		status = "× Failed"
	case "skipped":
		status = "− Skipped"
	case "pending":
		status = "○ Pending"
	case "waiting_review", "Review before running":
		status = "? Awaiting approval"
	case "cancelled":
		status = "× Cancelled"
	case "interrupted":
		status = "! Interrupted"
	}
	return append(rows, rule, line(" "+clean(status), normal), bottom)
}
