// Package popup owns the framed surface every overlay shares: the command
// completion list, the session search, the header menus and the info dialog.
//
// Four surfaces drew the same rounded border, the same side characters and
// the same surface-restoration hack, and drifted: two of them padded content
// by a cell inside the frame and two did not, so a row that lined up in one
// misaligned in the other.
package popup

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

// ModalWidth is shared by task modals; anchored menus size to content.
func ModalWidth(areaW int) int { return max(4, min(80, areaW-2)) }

// Styles are the palette values a popup draws with. The caller supplies them
// so this package stays free of the application's theme.
type Styles struct {
	Normal   lipgloss.Style
	Border   lipgloss.Style
	Selected lipgloss.Style
}

// Row renders one line of content at exactly width cells.
//
// Nested text styles reset SGR, which drops the popup's background colour
// and lets the terminal show through mid-row. Each reset is rewritten to
// restore the surface first, and a final reset keeps that colour inside the
// popup's own cells rather than bleeding onto the frame.
//
// Callers compose their own rows — a list highlights the selection, so the
// row style cannot be decided here — and hand the result to Frame.
func Row(style lipgloss.Style, text string, width int) string {
	r, g, b, _ := style.GetBackground().RGBA()
	restore := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
	styled := style.Render(cell.Line(text, width))
	restored := strings.NewReplacer(
		"\x1b[m", "\x1b[m"+restore,
		"\x1b[0m", "\x1b[0m"+restore,
		"\x1b[49m", "\x1b[49m"+restore,
	).Replace(styled)
	return restored + "\x1b[m"
}

// Frame wraps already-rendered rows in a rounded border. width is the whole
// frame, edge to edge; rows are expected to fill width-4 cells.
func Frame(rows []string, width int, s Styles) []string {
	return frame(rows, "", width, s)
}

// TitledFrame is Frame with a title set into the top border. A title too long
// for the requested width is clipped so it cannot escape the terminal.
func TitledFrame(title string, rows []string, width int, s Styles) []string {
	return frame(rows, title, width, s)
}

// Box renders rows with the normal surface and frames them, sizing the frame
// to the widest row. This is for a dialog, which draws its rows itself.
func Box(rows []string, s Styles) []string {
	width := cell.Width(strings.Join(rows, "\n")) + 4
	inner := max(width-4, 1)
	framed := make([]string, 0, len(rows)+2)
	framed = append(framed, s.Border.Render(topBorder("", width)))
	for _, row := range rows {
		framed = append(framed, s.Border.Render("│ ")+Row(s.Normal, row, inner)+s.Border.Render(" │"))
	}
	framed = append(framed, bottom(width, s))
	return framed
}

// ModalBox uses the common task-modal width for both drawing and hit testing.
func ModalBox(rows []string, areaW int, s Styles) []string {
	width := ModalWidth(areaW)
	styled := make([]string, len(rows))
	for i, row := range rows {
		styled[i] = Row(s.Normal, row, max(width-4, 1))
	}
	return Frame(styled, width, s)
}

// FrameBox centers a box in a larger area, which is how a dialog takes over
// the transcript without splicing ANSI into the lines beneath it.
//
// Every line is indented, not just the first: indenting only the first
// shifts the top border away from the box it belongs to, which reads as two
// overlapping frames rather than one misaligned one.
func FrameBox(rows []string, areaW, areaH int, s Styles) string {
	framed := ModalBox(rows, areaW, s)
	w := cell.Width(strings.Join(framed, "\n"))
	left := max((areaW-w)/2, 0)
	top := max((areaH-len(framed))/2, 0)

	indent := strings.Repeat(" ", left)
	indented := make([]string, len(framed))
	for i, line := range framed {
		indented[i] = indent + line
	}
	return strings.Repeat("\n", top) + strings.Join(indented, "\n")
}

// OverlayBox shares the modal frame and centering with FrameBox while keeping
// the underlying document visible outside the frame.
func OverlayBox(background string, rows []string, areaW, areaH int, s Styles) string {
	framed := ModalBox(rows, areaW, s)
	width := ModalWidth(areaW)
	return Overlay(background, framed, max((areaW-width)/2, 0), max((areaH-len(framed))/2, 0), width, areaW)
}

// Overlay splices rows over an already-rendered screen at a position,
// cutting the rows either side of the hole rather than overwriting whole
// lines.
//
// This is the one piece of overlay chrome that does not live in a component,
// and it was written twice in the root package with only the variable names
// changed - which is the failure mode the package exists to prevent. The cut
// is ANSI-aware, so a styled cell on the far side of the hole survives.
//
// width is the overlay's declared width, not the width of whichever row
// happens to be shorter: the columns between the row's end and the frame are
// background, and cutting from the row's own end would eat them.
//
// Rows past the end of the screen are dropped rather than appended: the
// background is already the full terminal height.
func Overlay(background string, rows []string, x, y, width, termW int) string {
	lines := strings.Split(background, "\n")
	for i, row := range rows {
		at := y + i
		if at < 0 || at >= len(lines) {
			continue
		}
		lines[at] = ansi.Cut(lines[at], 0, x) + row + ansi.Cut(lines[at], x+width, termW)
	}
	return strings.Join(lines, "\n")
}

func frame(rows []string, title string, width int, s Styles) []string {
	framed := make([]string, 0, len(rows)+2)
	framed = append(framed, s.Border.Render(topBorder(title, width)))
	for _, row := range rows {
		framed = append(framed, s.Border.Render("│ ")+row+s.Border.Render(" │"))
	}
	framed = append(framed, bottom(width, s))
	return framed
}

func bottom(width int, s Styles) string {
	return s.Border.Render("╰" + strings.Repeat("─", max(width-2, 2)) + "╯")
}

func topBorder(title string, width int) string {
	edge := max(width-2, 2)
	if title == "" {
		return "╭" + strings.Repeat("─", edge) + "╮"
	}
	label := ansi.Truncate("─ "+title+" ", edge, "…")
	return "╭" + label + strings.Repeat("─", max(edge-lipgloss.Width(label), 0)) + "╮"
}
