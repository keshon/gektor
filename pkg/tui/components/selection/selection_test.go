package selection

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPagingFilteringAndEmptyLists(t *testing.T) {
	m := Model{}
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown}, 20, 4)
	if m.Cursor != 4 || m.Offset != 1 {
		t.Fatalf("page: %+v", m)
	}
	m.Clamp(2, 4)
	if m.Cursor != 1 || m.Offset != 0 {
		t.Fatalf("filter: %+v", m)
	}
	m.Move(9, 0, 0)
	if m.Cursor != 0 || m.Offset != 0 {
		t.Fatalf("empty: %+v", m)
	}
	start, end := m.Centered(0, 0)
	if start != 0 || end != 0 {
		t.Fatal("empty window")
	}
}
