// Package completion owns the slash-command and path picker: the item list,
// the selection, and the framed list that replaces the bottom of the
// transcript.
//
// The command list used to be a flat list of names shared by help and the
// picker, with no notion of whether a command still needed an argument — so
// the picker could not tell "/help", which is done, from "/max-steps",
// which is not. The registry lives with the commands; this owns how they are
// chosen and drawn.
package completion

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

// Item is one candidate: what is inserted, and what it is.
type Item struct {
	Value       string
	Description string
	Directory   bool
}

// State is the open picker. The parent owns the token boundaries and decides
// what accepting an item means.
type State struct {
	Items    []Item
	Start    int
	End      int
	Selected int

	key       string
	dismissed string
}

// SetKey records the key and resets the selection, returning false when
// nothing changed.
//
// A dismissal is remembered until the text actually differs from what was
// dismissed. Clearing it here instead would undo every Esc on the very next
// sync, because the key is unchanged and that sync is what re-asks.
func (s *State) SetKey(key string) bool {
	if key == s.key {
		return false
	}
	s.key, s.Selected = key, 0
	if s.dismissed != "" && s.dismissed != key {
		s.dismissed = ""
	}
	return true
}

// Open replaces the candidates unless this exact key was dismissed.
func (s *State) Open(items []Item, key string) {
	s.SetKey(key)
	if key == s.dismissed {
		s.Items = nil
		return
	}
	s.Items = items
}

// Close empties the picker without remembering the key, for the paths that
// simply stop matching: an overlay opened, a run started, the draft cleared.
func (s *State) Close() {
	s.Items, s.key, s.dismissed = nil, "", ""
}

// Dismiss closes the picker and remembers the key, so the same text does not
// reopen it under the caret.
func (s *State) Dismiss() {
	s.dismissed, s.Items = s.key, nil
}

// Pending reports whether a command or path is waiting at the caret.
func (s *State) Pending() bool { return len(s.Items) > 0 }

// Bounds are the rune range the token covers in the composer, set by the
// caller that found the token.
func (s *State) Bounds() (start, end int) { return s.Start, s.End }

// Keypress moves the selection. It reports whether the picker consumed the
// key, so the composer never sees the same Enter.
func (s *State) Keypress(msg tea.KeyPressMsg) bool {
	if !s.Pending() {
		return false
	}
	switch msg.String() {
	case "up":
		s.Selected = (s.Selected + len(s.Items) - 1) % len(s.Items)
	case "down":
		s.Selected = (s.Selected + 1) % len(s.Items)
	case "esc":
		s.Dismiss()
	default:
		return false
	}
	return true
}

// Current is the selected item.
func (s *State) Current() Item {
	return s.Items[min(s.Selected, len(s.Items)-1)]
}

// View draws the picker framed at the given width, returning the rows. The
// caller splices them over the bottom of the transcript, so the frame never
// grows taller than the area it sits in: height bounds the visible rows.
//
// The selected row is styled here rather than by the frame, because the row
// style is the whole point of a list and the frame does not know what is
// selected.
func (s *State) View(width, height int, styles popup.Styles) []string {
	if !s.Pending() || width < 24 || height < 4 {
		return nil
	}
	title := "Files"
	if strings.HasPrefix(s.Items[0].Value, "/") {
		title = "Commands"
	}
	// Two border rows plus the hint row are not list rows.
	count := min(min(maxVisible, height-3), len(s.Items))
	if count < 1 {
		return nil
	}
	start := min(max(s.Selected-count+1, 0), len(s.Items)-count)
	inner := max(width-4, 1)

	rows := make([]string, 0, count+1)
	for i := start; i < start+count; i++ {
		item := s.Items[i]
		mark, style := "  ", styles.Normal
		if i == s.Selected {
			mark, style = "› ", styles.Selected
		}
		label := mark + cell.Line(item.Value, valueWidth(s.Items, width)) + "  " + item.Description
		rows = append(rows, popup.Row(style, label, inner))
	}
	hint := "↑↓ select  " + s.Action(s.runsOnEnter) + "  Esc dismiss"
	rows = append(rows, popup.Row(styles.Normal, "  "+hint, inner))
	return popup.TitledFrame(title, rows, width, styles)
}

// runsOnEnter reports whether Enter on this item would run it rather than
// insert it. The hint under the list uses it so the list cannot promise
// something Enter will not do.
func (s *State) runsOnEnter(item Item) bool {
	return !item.Directory && strings.HasPrefix(item.Value, "/") && !TakesArgument(item.Value)
}

// TakesArgument reports whether Enter should hand the command to the
// composer instead of running it. The caller injects this because the
// registry belongs to the command layer, not to a picker.
var TakesArgument = func(string) bool { return false }

// SetRunner supplies the registry lookup the hint needs.
func SetRunner(fn func(string) bool) { TakesArgument = fn }

// Action names what Enter does with the selected item.
func (s *State) Action(runnable func(Item) bool) string {
	if runnable != nil && runnable(s.Current()) {
		return "Enter run  Tab insert"
	}
	return "Enter insert  Tab insert"
}

// maxVisible keeps the list from swallowing the transcript it sits over.
const maxVisible = 12

// valueWidth is the widest label in the visible set, capped at half the
// popup so a long path cannot push every description off the edge.
func valueWidth(items []Item, width int) int {
	w := 0
	for _, item := range items {
		if n := lipgloss.Width(item.Value); n > w {
			w = n
		}
	}
	return min(w, max((width-6)/2, 1))
}
