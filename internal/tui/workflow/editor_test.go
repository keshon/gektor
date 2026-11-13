package workflow

import (
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/workflow"
)

func TestConnectionsUndoAndGeometry(t *testing.T) {
	e := NewEditor()
	e.Definition.Steps = []workflow.Step{{ID: "a", Tool: "x", Args: json.RawMessage(`{}`)}, {ID: "b", Tool: "x", Args: json.RawMessage(`{}`)}}
	e.Saved = e.Source()
	if err := e.Link("a", "b", "input", "/data"); err != nil {
		t.Fatal(err)
	}
	linked := e.Source()
	if err := e.Link("b", "a", "", ""); err == nil {
		t.Fatal("cycle accepted")
	}
	if e.Source() != linked {
		t.Fatal("failed edit mutated draft")
	}
	g := e.Graph(graph.Model{})
	if len(g.Edges) != 1 || !g.Edges[0].Data {
		t.Fatal(g.Edges)
	}
	g.Move(10, 2)
	if e.Source() != linked {
		t.Fatal("geometry changed executable source")
	}
	e.Undo()
	if e.Dirty() {
		t.Fatal("undo did not restore saved state")
	}
	e.Redo()
	if e.Source() != linked {
		t.Fatal("redo lost connection")
	}
}
func TestDecodeRejectsUnknownAndTrailingData(t *testing.T) {
	for _, s := range []string{`{"version":4,"mystery":1}`, `{"version":4} {}`, `{"version":2}`, `{"version":3}`} {
		if _, err := Decode([]byte(s)); err == nil {
			t.Fatal(s)
		}
	}
}
