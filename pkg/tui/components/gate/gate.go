// Package gate owns the permission/ask/plan bottom popup: its options,
// its selection, and the framed list spliced over the bottom of the
// transcript.
//
// The interaction answers stay where they were (gates.go): this owns
// how options are chosen and drawn, the same split as completion —
// the picker draws, the caller decides what accepting means.
package gate

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/pkg/tui/components/cell"
	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/components/popup"
)

// Option is one answer row: what it sends, and what it says.
type Option struct {
	// Value is the answer the gate resolves with.
	Value string
	// Label is what the row shows.
	Label string
	// Hint names the shortcut key, "" when the row has none.
	Hint string
}

// State is the open gate popup. The parent owns answering and decides
// what accepting an option means.
type State struct {
	Title   string
	Options []Option
	// Selected is the highlighted row.
	Selected int
}

// Open replaces the options and resets the selection.
func (s *State) Open(title string, options []Option) {
	s.Title, s.Options, s.Selected = title, options, 0
}

// Close empties the popup.
func (s *State) Close() { s.Options, s.Selected = nil, 0 }

// Pending reports whether a gate popup is open.
func (s *State) Pending() bool { return len(s.Options) > 0 }

// Current is the selected option.
func (s *State) Current() Option {
	if !s.Pending() {
		return Option{}
	}

	return s.Options[max(0, min(s.Selected, len(s.Options)-1))]
}

// Move shifts the selection, wrapping at both ends. Which rows are visible
// is settled by View, which is the only place that knows the height; keeping
// a second guess here meant a list scrolled on a guess the frame never made.
func (s *State) Move(delta int) {
	if !s.Pending() {
		return
	}

	n := len(s.Options)
	s.Selected = ((s.Selected+delta)%n + n) % n
}

// Keypress moves the selection. It reports whether the popup consumed
// the key, so the composer never sees the same Enter.
func (s *State) Keypress(msg tea.KeyPressMsg) bool {
	if !s.Pending() {
		return false
	}

	switch msg.String() {
	case "up":
		s.Move(-1)
	case "down":
		s.Move(1)
	default:
		return false
	}

	return true
}

// Layout contains the frame and the visible option rows in frame coordinates.
type Layout struct {
	Rows        []string
	Options     pane.Rect
	FirstOption int
}

// Hit returns the option index at a point inside the framed list.
func (l Layout) Hit(x, y int) (int, bool) {
	return l.FirstOption + y - l.Options.Y, l.Options.Contains(x, y)
}

// View draws the framed popup rows.
func (s *State) View(width, height int, question []string, hint string, note bool, styles popup.Styles) []string {
	return s.Layout(width, height, question, hint, note, styles).Rows
}

// Layout fits the prompt and choices within the available transcript rows.
func (s *State) Layout(width, height int, question []string, hint string, note bool, styles popup.Styles) Layout {
	if !s.Pending() || width < 24 || height < 4 {
		return Layout{}
	}

	inner := max(width-4, 1)

	// Keep the first prompt line visible even when only the selected
	// choice fits. Keyboard navigation can reveal the remaining choices.
	promptRows := 0
	if len(question) > 0 {
		promptRows = 1
	}
	base := 2 + 1 + promptRows
	showHint := height > base
	if showHint {
		base++
	}

	divided := height >= 8
	if divided {
		base++
	}

	if height < base {
		return Layout{}
	}

	free := height - base
	noteShown := note && free > 0
	if noteShown {
		free--
	}

	count := min(min(maxVisible, 1+free), len(s.Options))
	free -= count - 1

	free += promptRows
	qrows := len(question)
	more := ""

	switch {
	case qrows <= free:
		// The whole question fits; nothing to report.
	case free == 1:
		// Keep the target visible when only one prompt row fits. A generic
		// truncation notice cannot identify what the user is approving.
		qrows = 1
	default:
		qrows = free - 1
		more = popup.Row(styles.Normal,
			fmt.Sprintf("  … %d more lines in the transcript above", len(question)-qrows), inner)
	}

	selected := max(0, min(s.Selected, len(s.Options)-1))
	offset := min(max(selected-count+1, 0), len(s.Options)-count)

	rows := make([]string, 0, height)

	for _, q := range question[:qrows] {
		rows = append(rows, popup.Row(styles.Normal, q, inner))
	}

	if more != "" {
		rows = append(rows, more)
	}

	options := pane.Rect{X: 2, Y: 1 + len(rows), Width: inner, Height: count}
	for i := offset; i < offset+count; i++ {
		opt := s.Options[i]
		mark, style := "  ", styles.Normal
		if i == selected {
			mark, style = "› ", styles.Selected
		}

		label := mark + cell.Line(opt.Label, labelWidth(s.Options, width))
		if opt.Hint != "" {
			label += "  " + opt.Hint
		}

		rows = append(rows, popup.Row(style, label, inner))
	}

	if noteShown {
		rows = append(rows, popup.Row(styles.Normal, "  note> type and press Enter", inner))
	}

	if divided {
		rows = append(rows, popup.Footer(inner, hint, styles.Normal)...)
	} else if showHint {
		rows = append(rows, popup.Row(styles.Normal, hint, inner))
	}

	return Layout{Rows: popup.TitledFrame(s.Title, rows, width, styles), Options: options, FirstOption: offset}
}

// maxVisible keeps the list from swallowing the transcript it sits over.
const maxVisible = 8

// labelWidth is the widest option label in the visible set, capped at
// half the popup so a long label cannot push every hint off the edge.
func labelWidth(options []Option, width int) int {
	w := 0

	for _, opt := range options {
		if n := cell.Width(opt.Label); n > w {
			w = n
		}
	}

	return min(max(w, 1), max((width-6)/2, 1))
}

// Wrap splits prompt text to the popup width.
func Wrap(prompt string, width int) []string {
	return cell.Wrap(prompt, width)
}
