package workflow

import (
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/automation"
)

func connectedEditor() *Editor {
	e := New()
	e.Definition.Steps = []automation.Step{
		{ID: "a", Tool: "test", Args: json.RawMessage(`{}`)},
		{ID: "b", Tool: "test", DependsOn: []string{"a"}, Args: json.RawMessage(`{"input":[{"$step":"a","pointer":"/first"}],"exact":9007199254740993}`), When: json.RawMessage(`{"$step":"a","pointer":"/ready"}`), ForEach: json.RawMessage(`{"$step":"a","pointer":"/items"}`)},
	}
	e.Saved = e.Source()
	return e
}

func TestNestedConnectionsEditRemoveAndExactUndo(t *testing.T) {
	for _, field := range []string{"args", "when", "for_each", "depends_on"} {
		t.Run(field, func(t *testing.T) {
			e := connectedEditor()
			before := e.Source()
			links := e.Connections("b")
			if len(links) != 4 || len(e.Connections("a")) != 4 {
				t.Fatal(links)
			}
			var c Connection
			for _, link := range links {
				if link.Field == field {
					c = link
				}
			}
			if field != "depends_on" {
				next := c
				next.Pointer = "/changed"
				if err := e.ChangeConnection(&c, &next); err != nil {
					t.Fatal(err)
				}
				e.Undo()
				if e.Source() != before {
					t.Fatal("edit undo lost data")
				}
			}
			if err := e.ChangeConnection(&c, nil); err != nil {
				t.Fatal(err)
			}
			removed := e.Source()
			if len(e.Connections("b")) != 3 {
				t.Fatal(e.Connections("b"))
			}
			e.Undo()
			if e.Source() != before {
				t.Fatal("remove undo lost data")
			}
			e.Redo()
			if e.Source() != removed {
				t.Fatal("redo changed data")
			}
		})
	}
}

func TestConnectionChangesRejectCyclesStaleLinksAndInvalidPointers(t *testing.T) {
	e := connectedEditor()
	before := e.Source()
	for _, c := range []Connection{
		{From: "b", To: "a", Field: "args", Path: "/input"},
		{From: "missing", To: "b", Field: "depends_on"},
		{From: "a", To: "b", Field: "args", Path: "/input", Pointer: "/bad~2"},
	} {
		if err := e.ChangeConnection(nil, &c); err == nil {
			t.Fatal("accepted invalid connection", c)
		}
		if e.Source() != before {
			t.Fatal("failed edit mutated draft")
		}
	}
	c := e.Connections("b")[0]
	c.From = "stale"
	if err := e.ChangeConnection(&c, nil); err == nil {
		t.Fatal("removed stale link")
	}
	if err := e.Link("a", "b", "", ""); err != nil {
		t.Fatal(err)
	}
	if e.Source() != before {
		t.Fatal("duplicate ordering edge added")
	}
}

func TestRemovingRequiredArgumentDoesNotInventReplacement(t *testing.T) {
	e := New()
	e.Definition.Steps = []automation.Step{{ID: "a", Args: json.RawMessage(`{}`)}, {ID: "b", Args: json.RawMessage(`{"input":{"$step":"a"},"exact":9007199254740993}`)}}
	c := e.Connections("b")[0]
	if err := e.ChangeConnection(&c, nil); err != nil {
		t.Fatal(err)
	}
	if string(e.Definition.Steps[1].Args) != `{"exact":9007199254740993}` {
		t.Fatal(string(e.Definition.Steps[1].Args))
	}
}
