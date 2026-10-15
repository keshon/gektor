// Package selection owns list selection and scroll geometry. Data and actions belong to its parent.
package selection

import tea "charm.land/bubbletea/v2"

type Model struct{ Cursor, Offset int }

func (m *Model) Clamp(count, capacity int) {
	capacity = max(capacity, 1)
	m.Cursor = min(max(m.Cursor, 0), max(count-1, 0))
	if m.Offset > m.Cursor {
		m.Offset = m.Cursor
	}
	if m.Cursor >= m.Offset+capacity {
		m.Offset = m.Cursor - capacity + 1
	}
	m.Offset = min(max(m.Offset, 0), max(count-capacity, 0))
}
func (m *Model) Move(delta, count, capacity int) { m.Cursor += delta; m.Clamp(count, capacity) }
func (m Model) Centered(count, capacity int) (int, int) {
	capacity = max(capacity, 1)
	start := max(min(m.Cursor-capacity/2, count-capacity), 0)
	return start, min(start+capacity, count)
}
func (m *Model) Update(msg tea.Msg, count, capacity int) bool {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false
	}
	switch key.String() {
	case "up":
		m.Move(-1, count, capacity)
	case "down":
		m.Move(1, count, capacity)
	case "pgup":
		m.Move(-max(capacity, 1), count, capacity)
	case "pgdown":
		m.Move(max(capacity, 1), count, capacity)
	default:
		return false
	}
	return true
}
