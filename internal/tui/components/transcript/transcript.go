// Package transcript displays rendered document parts while preserving the reader's position.
package transcript

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type Model struct {
	Viewport viewport.Model
	Follow   bool
	lines    []int
	// lastParts/lastTail are what the viewport currently shows. The
	// renderer rebuilds its parts on every frame, including frames that
	// changed nothing, so this is what tells a real update from a repeat.
	lastParts []string
	lastTail  string
}

func New() Model             { return Model{Follow: true} }
func (m Model) View() string { return m.Viewport.View() }
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	m.Follow = m.Viewport.AtBottom()
	return cmd
}
func (m Model) PartOffset(index int) int {
	row := 0
	for i := 0; i < index && i < len(m.lines); i++ {
		row += m.lines[i] + 1
	}
	return row
}

// SetParts accepts rendered, immutable strings. Tail is an unsettled streamed part.
//
// Identical input produces identical output, so a frame that re-renders to
// the same parts skips the whole reassembly. That path is not free: the
// viewport splits the joined content into lines and measures the widest of
// them with an ANSI-aware scan on every call.
func (m *Model) SetParts(parts []string, tail string) {
	if m.lastTail == tail && slices.Equal(m.lastParts, parts) {
		return
	}
	content := strings.Join(parts, "\n\n")
	if tail != "" {
		if content != "" {
			content += "\n\n"
		}
		content += tail
	}
	heights := make([]int, len(parts))
	for i, p := range parts {
		heights[i] = strings.Count(p, "\n") + 1
	}
	if !m.Follow && len(heights) > 0 && len(heights) == len(m.lines) {
		oldTop := m.Viewport.YOffset()
		acc, index := 0, 0
		for index < len(m.lines) && acc+m.lines[index] <= oldTop {
			acc += m.lines[index] + 1
			index++
		}
		shift := 0
		for j := 0; j < index && j < len(heights); j++ {
			shift += heights[j] - m.lines[j]
		}
		m.Viewport.SetContent(content)
		offset := oldTop + shift
		if maxOffset := strings.Count(content, "\n") + 1 - m.Viewport.Height(); maxOffset > 0 && offset > maxOffset {
			offset = maxOffset
		}
		m.Viewport.SetYOffset(max(offset, 0))
	} else {
		m.Viewport.SetContent(content)
		if m.Follow {
			m.Viewport.GotoBottom()
		} else if len(heights) == 0 {
			m.Viewport.SetYOffset(0)
		}
	}
	m.lastParts, m.lastTail = parts, tail
	m.lines = heights
}
