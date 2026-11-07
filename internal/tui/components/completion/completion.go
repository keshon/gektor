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
	drag      bool
	grab      int
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
	s.key, s.Selected, s.drag = key, 0, false
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
	s.Selected = min(max(s.Selected, 0), max(len(items)-1, 0))
}

// Close empties the picker without remembering the key, for the paths that
// simply stop matching: an overlay opened, a run started, the draft cleared.
func (s *State) Close() {
	s.Items, s.key, s.dismissed = nil, "", ""
	s.drag = false
}

// Dismiss closes the picker and remembers the key, so the same text does not
// reopen it under the caret.
func (s *State) Dismiss() {
	s.dismissed, s.Items = s.key, nil
	s.drag = false
}

// Pending reports whether a command or path is waiting at the caret.
func (s *State) Pending() bool { return len(s.Items) > 0 }

// Bounds are the rune range the token covers in the composer, set by the
// caller that found the token.
func (s *State) Bounds() (start, end int) { return s.Start, s.End }

// Keypress moves the selection. It reports whether the picker consumed the
// key, so the composer never sees the same Enter.
func (s *State) Keypress(msg tea.KeyPressMsg, page int) bool {
	if !s.Pending() {
		return false
	}
	switch msg.String() {
	case "up":
		s.Selected = (s.Selected + len(s.Items) - 1) % len(s.Items)
	case "down":
		s.Selected = (s.Selected + 1) % len(s.Items)
	case "pgup":
		s.Selected = max(s.Selected-max(page, 1), 0)
	case "pgdown":
		s.Selected = min(s.Selected+max(page, 1), len(s.Items)-1)
	case "esc":
		s.Dismiss()
	default:
		return false
	}
	return true
}

// Current is the selected item.
func (s *State) Current() Item {
	if !s.Pending() {
		return Item{}
	}
	return s.Items[min(max(s.Selected, 0), len(s.Items)-1)]
}

type Props struct {
	Width, Height int
	Surface       popup.Styles
	Title         lipgloss.Style
	Shortcuts     popup.ShortcutStyles
	Runnable      func(Item) bool
}

type Layout struct {
	Rows                               []string
	Width, BodyTop, BodyHeight, Offset int
	Scrollbar                          bool
	Close                              bool
}

// Layout anchors the list's header, visible rows and footer inside one frame.
func (s *State) Layout(p Props) Layout {
	l := Layout{Width: p.Width, BodyTop: 3}
	if !s.Pending() || p.Width < 24 || p.Height < 4 {
		return l
	}
	reserved := 6
	l.Close = true
	if p.Height < 7 {
		reserved, l.BodyTop, l.Close = 3, 1, false
	}
	count := min(maxVisible, p.Height-reserved, len(s.Items))
	selected := min(max(s.Selected, 0), len(s.Items)-1)
	l.Offset = min(max(selected-count+1, 0), len(s.Items)-count)
	l.BodyHeight, l.Scrollbar = count, len(s.Items) > count
	inner := p.Width - 4
	title := "Files"
	if strings.HasPrefix(s.Items[0].Value, "/") {
		title = "Commands"
	}
	rows := popup.Header(title, inner, p.Title.Inherit(p.Surface.Normal).Bold(true))
	rows[0] = popup.Row(p.Surface.Normal, rows[0], inner)
	body := []string{}
	valueW := valueWidth(s.Items[l.Offset:l.Offset+count], p.Width)
	for i := l.Offset; i < l.Offset+count; i++ {
		item := s.Items[i]
		mark, style := "  ", p.Surface.Normal
		if i == selected {
			mark, style = "› ", p.Surface.Selected
		}
		label := mark + cell.Line(item.Value, valueW) + "  " + item.Description
		body = append(body, popup.Row(style, label, inner))
	}
	body = popup.ScrollRows(body, inner, l.Offset, len(s.Items), p.Surface.Normal)
	rows = append(rows, body...)
	action := "insert"
	if p.Runnable != nil && p.Runnable(s.Current()) {
		action = "run"
	}
	hint := popup.Shortcuts(inner, p.Shortcuts,
		popup.Shortcut{Key: "Esc", Action: "close"}, popup.Shortcut{Key: "Enter", Action: action},
		popup.Shortcut{Key: "Tab", Action: "insert"}, popup.Shortcut{Key: "↑↓", Action: "select"}, popup.Shortcut{Key: "PgUp/PgDn", Action: "page"})
	hint = popup.Row(p.Surface.Normal, hint, inner)
	rows = append(rows, popup.Footer(inner, hint, lipgloss.NewStyle())...)
	if l.Close {
		l.Rows = popup.Frame(rows, p.Width, p.Surface)
	} else {
		body = append(body, hint)
		l.Rows = popup.TitledFrame(title, body, p.Width, p.Surface)
	}
	return l
}

func (l Layout) Contains(x, y int) bool {
	return len(l.Rows) > 0 && x >= 0 && x < l.Width && y >= 0 && y < len(l.Rows)
}
func (l Layout) Hit(x, y int) int {
	right := l.Width - 2
	if l.Scrollbar {
		right -= 2
	}
	if x < 2 || x >= right || y < l.BodyTop || y >= l.BodyTop+l.BodyHeight {
		return -1
	}
	return l.Offset + y - l.BodyTop
}

// Mouse selects candidates without inserting text or executing commands.
// The owning screen supplies coordinates relative to the rendered frame.
func (s *State) Mouse(msg tea.MouseMsg, l Layout) bool {
	mouse := msg.Mouse()
	if s.drag {
		if l.BodyHeight < 1 {
			s.drag = false
			return true
		}
		switch msg.(type) {
		case tea.MouseMotionMsg, tea.MouseReleaseMsg:
			offset := popup.ScrollOffset(mouse.Y-l.BodyTop, l.BodyHeight, len(s.Items), s.grab)
			s.Selected = min(offset+l.BodyHeight-1, len(s.Items)-1)
			if _, released := msg.(tea.MouseReleaseMsg); released {
				s.drag = false
			}
			return true
		}
	}
	if !l.Contains(mouse.X, mouse.Y) {
		return false
	}
	if _, click := msg.(tea.MouseClickMsg); click && mouse.Button == tea.MouseLeft {
		if l.Close && mouse.Y == 1 && mouse.X >= l.Width-5 && mouse.X < l.Width-2 {
			s.Dismiss()
		} else if l.Scrollbar && mouse.X == l.Width-3 && mouse.Y >= l.BodyTop && mouse.Y < l.BodyTop+l.BodyHeight {
			row := mouse.Y - l.BodyTop
			top, size := popup.ScrollThumb(l.BodyHeight, len(s.Items), l.Offset)
			if row >= top && row < top+size {
				s.drag, s.grab = true, row-top
			} else {
				key := tea.KeyPgDown
				if row < top {
					key = tea.KeyPgUp
				}
				s.Keypress(tea.KeyPressMsg{Code: key}, l.BodyHeight)
			}
		} else if i := l.Hit(mouse.X, mouse.Y); i >= 0 {
			s.Selected = i
		}
	}
	if l.Hit(mouse.X, mouse.Y) >= 0 {
		switch mouse.Button {
		case tea.MouseWheelUp:
			s.Keypress(tea.KeyPressMsg{Code: tea.KeyUp}, 1)
		case tea.MouseWheelDown:
			s.Keypress(tea.KeyPressMsg{Code: tea.KeyDown}, 1)
		}
	}
	return true
}

func (s *State) Dragging() bool { return s.Pending() && s.drag }

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
