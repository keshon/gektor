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
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/popup"
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
	// offset is the first visible option for long lists.
	offset int
}

// Open replaces the options and resets the selection.
func (s *State) Open(title string, options []Option) {
	s.Title, s.Options, s.Selected, s.offset = title, options, 0, 0
}

// Close empties the popup.
func (s *State) Close() { s.Options, s.Selected, s.offset = nil, 0, 0 }

// Pending reports whether a gate popup is open.
func (s *State) Pending() bool { return len(s.Options) > 0 }

// Current is the selected option.
func (s *State) Current() Option { return s.Options[min(s.Selected, len(s.Options)-1)] }

// Move shifts the selection, wrapping at both ends. Which rows are visible
// is settled by View, which is the only place that knows the height; keeping
// a second guess here meant a list scrolled on a guess the frame never made.
func (s *State) Move(delta int) {
	if !s.Pending() {
		return
	}
	s.Selected = (s.Selected + delta + len(s.Options)) % len(s.Options)
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

// View draws the popup framed at the given width, returning the rows.
// The caller splices them over the bottom of the transcript, so the frame
// must never grow taller than the area it sits in: height is a hard
// ceiling, not a hint. question carries the wrapped prompt lines; hint is
// the single footer row naming the keys.
//
// The question head stays pinned: options and hint are always visible, and
// a long prompt truncates with a count rather than pushing the answers off.
// The full prompt is also in the transcript behind the popup, so nothing is
// lost — scroll there for the rest.
func (s *State) View(width, height int, question []string, hint string, note bool, styles popup.Styles) []string {
	if !s.Pending() || width < 24 || height < 4 {
		return nil
	}
	inner := max(width-4, 1)

	// Spend the height explicitly. Overshooting is not a cosmetic bug: the
	// caller splices these rows over a fixed-height body and drops the whole
	// popup when it does not fit, so a single row too many makes the gate
	// vanish and leaves a run blocked behind a question nobody can see.
	//
	// Two borders, one option and the hint are not negotiable; everything
	// else is spent in order of what the reader needs: the note row, then
	// the other options, then the question.
	const base = 2 + 1 + 1
	if height < base {
		return nil
	}
	free := height - base
	noteShown := note && free > 0
	if noteShown {
		free--
	}
	count := min(min(maxVisible, 1+free), len(s.Options))
	free -= count - 1

	qrows := len(question)
	more := ""
	switch {
	case qrows <= free:
		// The whole question fits; nothing to report.
	case free <= 0:
		// No room at all. The answers win, and the prompt is in the
		// transcript behind, which the caller has made scrollable.
		qrows = 0
	case free == 1:
		qrows, more = 0, popup.Row(styles.Normal,
			"  … prompt is in the transcript above", inner)
	default:
		qrows = free - 1
		more = popup.Row(styles.Normal,
			fmt.Sprintf("  … %d more lines in the transcript above", len(question)-qrows), inner)
	}
	s.offset = min(max(s.Selected-count+1, 0), len(s.Options)-count)

	rows := make([]string, 0, height)
	for _, q := range question[:qrows] {
		rows = append(rows, popup.Row(styles.Normal, q, inner))
	}
	if more != "" {
		rows = append(rows, more)
	}
	for i := s.offset; i < s.offset+count; i++ {
		opt := s.Options[i]
		mark, style := "  ", styles.Normal
		if i == s.Selected {
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
	rows = append(rows, popup.Row(styles.Normal, "  "+hint, inner))
	return popup.TitledFrame(s.Title, rows, width, styles)
}

// maxVisible keeps the list from swallowing the transcript it sits over.
const maxVisible = 8

// labelWidth is the widest option label in the visible set, capped at
// half the popup so a long label cannot push every hint off the edge.
func labelWidth(options []Option, width int) int {
	w := 0
	for _, opt := range options {
		if n := len([]rune(opt.Label)); n > w {
			w = n
		}
	}
	return min(max(w, 1), max((width-6)/2, 1))
}

// Wrap splits prompt text to the popup width.
func Wrap(prompt string, width int) []string {
	var out []string
	for _, line := range strings.Split(prompt, "\n") {
		for len([]rune(line)) > width && width > 0 {
			out = append(out, string([]rune(line)[:width]))
			line = string([]rune(line)[width:])
		}
		out = append(out, line)
	}
	return out
}
