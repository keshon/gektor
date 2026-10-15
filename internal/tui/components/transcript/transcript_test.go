package transcript

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

func TestReflowAnchorsReaderAndAppendDoesNotStealPosition(t *testing.T) {
	m := New()
	m.Viewport = viewport.New(viewport.WithWidth(30), viewport.WithHeight(3))
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
	m.Latest()
	if !m.Follow || !m.Viewport.AtBottom() {
		t.Fatal("latest did not resume following")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.Follow {
		t.Fatal("scroll did not pause following")
	}
}
func TestPartOffsetsAndTailDoNotDependOnViewport(t *testing.T) {
	m := New()
	m.Viewport = viewport.New(viewport.WithWidth(30), viewport.WithHeight(20))
	m.SetParts([]string{"one\ntwo", "three"}, "streaming")
	if m.PartOffset(1) != 3 || !strings.Contains(m.View(), "streaming") {
		t.Fatal("part offsets or tail missing")
	}
}
