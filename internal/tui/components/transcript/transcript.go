// Package transcript displays rendered document parts while preserving the reader's position.
package transcript

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type Model struct {
	Viewport Viewport
	Follow   bool
	// lastParts/lastTail are what the window currently shows. The renderer
	// hands over every block on every frame, including frames that changed
	// nothing, so this is what tells a real update from a repeat.
	lastParts []string
	lastTail  string
}

func New() Model { return Model{Follow: true, Viewport: NewViewport(0, 0)} }

func (m Model) View() string { return m.Viewport.View() }

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	cmd := m.Viewport.Update(msg)
	m.Follow = m.Viewport.AtBottom()
	return cmd
}

// PartOffset is the document line a part begins at, so a caller holding a
// block index can scroll to it.
func (m Model) PartOffset(index int) int {
	if index <= 0 {
		return 0
	}
	if index < len(m.Viewport.starts) {
		return m.Viewport.starts[index]
	}
	return m.Viewport.Total()
}

// SetParts accepts rendered, immutable strings. Tail is an unsettled streamed part.
//
// Parts are stored, not joined. The window needs a part's height to find the
// next one, and every part's height to know how far the document scrolls, but
// it never needs their text unless they are on screen.
func (m *Model) SetParts(parts []string, tail string) {
	if m.lastTail == tail && slices.Equal(m.lastParts, parts) {
		return
	}
	all := parts
	if tail != "" {
		all = make([]string, 0, len(parts)+1)
		all = append(all, parts...)
		all = append(all, tail)
	}

	starts := make([]int, len(all))
	line := 0
	for i, p := range all {
		if i > 0 {
			line++ // the blank line separating this part from the last
		}
		starts[i] = line
		line += strings.Count(p, "\n") + 1
	}
	total := line

	previous := m.Viewport
	if !m.Follow && len(parts) == len(previous.parts) {
		// Anchoring: the reader is inside some part, and that part is
		// still the same one. Keep them the same distance into it, so a
		// reflow above them moves the text under the reader rather than
		// the reader away from the text.
		if k := previous.partAt(previous.offset); k >= 0 && k < len(starts) {
			within := previous.offset - previous.starts[k]
			if within < 0 {
				within = 0
			}
			m.Viewport.Set(parts, starts, total)
			m.Viewport.SetYOffset(starts[k] + within)
			m.lastParts, m.lastTail = parts, tail
			return
		}
	}
	m.Viewport.Set(all, starts, total)
	if m.Follow {
		m.Viewport.GotoBottom()
	} else if len(parts) == 0 {
		m.Viewport.SetYOffset(0)
	}
	m.lastParts, m.lastTail = parts, tail
}
