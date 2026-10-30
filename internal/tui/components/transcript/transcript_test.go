package transcript

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestReflowAnchorsReaderAndAppendDoesNotStealPosition(t *testing.T) {
	m := New()
	m.Viewport = NewViewport(30, 3)
	m.SetParts([]string{"first", "second\nbody", "third\nbody\nend"}, "")
	m.Follow = false
	m.Viewport.SetYOffset(3)
	m.SetParts([]string{"first\nwrapped", "second\nbody", "third\nbody\nend"}, "")
	if m.Viewport.YOffset() != 4 {
		t.Fatalf("reflow moved reader: %d", m.Viewport.YOffset())
	}
	m.SetParts([]string{"first\nwrapped", "second\nbody", "third\nbody\nend", "new"}, "")
	if m.Viewport.YOffset() != 4 {
		t.Fatal("append stole position")
	}
	m.Viewport.GotoBottom()
	m.Follow = true
	if !m.Viewport.AtBottom() {
		t.Fatal("latest did not resume following")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.Follow {
		t.Fatal("scroll did not pause following")
	}
}

func TestPartOffsetsAndTailDoNotDependOnViewport(t *testing.T) {
	m := New()
	m.Viewport = NewViewport(30, 20)
	m.SetParts([]string{"one\ntwo", "three"}, "streaming")
	if m.PartOffset(1) != 3 || !strings.Contains(m.View(), "streaming") {
		t.Fatalf("part offsets or tail missing: offset=%d", m.PartOffset(1))
	}
}

func TestGroupedPartsKeepOffsetsAndReadingPosition(t *testing.T) {
	m := New()
	m.Viewport = NewViewport(30, 3)
	parts := []string{"reply", "tool one", "tool two", "next reply"}
	m.SetPartsWithGaps(parts, "streaming", []int{1, 1, 0, 1})
	if m.PartOffset(1) != 2 || m.PartOffset(2) != 3 || m.PartOffset(3) != 5 {
		t.Fatal("grouping lost individual part offsets")
	}
	m.Follow = false
	m.Viewport.SetYOffset(3)
	m.SetParts(parts, "streaming")
	if m.PartOffset(2) != 4 || m.Viewport.YOffset() != 4 {
		t.Fatal("spacing change moved the reader away from tool two")
	}
	m.SetPartsWithGaps(parts, "", []int{1, 1, 0, 1})
	m.Viewport.SetHeight(20)
	m.Viewport.GotoTop()
	if !strings.Contains(m.View(), "tool one                      \ntool two") {
		t.Fatal("grouped tool parts still have a blank separator", m.View())
	}
}

// The window exists to avoid splitting parts nobody is looking at. The only
// honest way to know that is happening is to hand it a part that would blow
// up if it were split, so this fails loudly rather than silently regressing.
func TestOnlyVisiblePartsAreSplit(t *testing.T) {
	m := New()
	m.Viewport = NewViewport(20, 3)
	parts := []string{"a\nb", "c\nd", "e\nf"}
	m.SetParts(parts, "")
	m.Viewport.SetYOffset(0)
	view := m.View()
	if strings.Count(view, "\n") != 2 {
		t.Fatalf("window is not %d rows:\n%s", 3, view)
	}
	if strings.Contains(view, "e") {
		t.Fatalf("a part below the window was drawn:\n%s", view)
	}
}
