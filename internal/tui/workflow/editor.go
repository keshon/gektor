// Package workflow owns editable recipes; rendering and execution live in adapters.
package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/recipe"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"io"
)

type Editor struct {
	Definition automation.Definition
	Path       string
	Saved      string
	undo, redo []string
}

func New() *Editor {
	e := &Editor{Definition: automation.Definition{Version: 3, Name: "Untitled workflow", Steps: []automation.Step{}}}
	e.Saved = e.Source()
	return e
}
func Decode(source []byte) (automation.Definition, error) {
	var d automation.Definition
	dec := json.NewDecoder(bytes.NewReader(source))
	dec.DisallowUnknownFields()
	err := dec.Decode(&d)
	if err != nil {
		return d, err
	}
	if dec.Decode(new(any)) != io.EOF {
		return d, fmt.Errorf("expected one recipe")
	}
	if d.Version != 3 {
		return d, fmt.Errorf("recipe version must be 3")
	}
	return d, nil
}
func (e *Editor) Source() string {
	b, _ := json.MarshalIndent(e.Definition, "", "  ")
	return string(b)
}
func (e *Editor) Dirty() bool { return e.Source() != e.Saved }
func (e *Editor) Replace(d automation.Definition) {
	e.undo = append(e.undo, e.Source())
	if len(e.undo) > 100 {
		e.undo = e.undo[1:]
	}
	e.redo = nil
	b, _ := json.Marshal(d)
	_ = json.Unmarshal(b, &e.Definition)
}
func (e *Editor) Undo() {
	if len(e.undo) == 0 {
		return
	}
	e.redo = append(e.redo, e.Source())
	e.Definition, _ = Decode([]byte(e.undo[len(e.undo)-1]))
	e.undo = e.undo[:len(e.undo)-1]
}
func (e *Editor) Redo() {
	if len(e.redo) == 0 {
		return
	}
	e.undo = append(e.undo, e.Source())
	e.Definition, _ = Decode([]byte(e.redo[len(e.redo)-1]))
	e.redo = e.redo[:len(e.redo)-1]
}
func (e *Editor) Clone() automation.Definition { d, _ := Decode([]byte(e.Source())); return d }
func (e *Editor) Link(from, to, arg, pointer string) error {
	d := e.Clone()
	found := false
	for i := range d.Steps {
		s := &d.Steps[i]
		if s.ID != to {
			continue
		}
		found = true
		if arg == "" {
			s.DependsOn = append(s.DependsOn, from)
		} else {
			var args map[string]json.RawMessage
			if err := json.Unmarshal(s.Args, &args); err != nil {
				return err
			}
			if args == nil {
				args = map[string]json.RawMessage{}
			}
			ref := map[string]string{"$step": from}
			if pointer != "" {
				ref["pointer"] = pointer
			}
			args[arg], _ = json.Marshal(ref)
			s.Args, _ = json.Marshal(args)
		}
	}
	if !found {
		return fmt.Errorf("destination missing")
	}
	if _, err := recipe.Order(d.Steps); err != nil {
		return err
	}
	e.Replace(d)
	return nil
}
func (e *Editor) Graph(old graph.Model) graph.Model {
	positions := map[string]graph.Point{}
	for _, n := range old.Nodes {
		positions[n.ID] = n.Point
	}
	// Place ready nodes in layers; execution order still belongs to recipe.Order.
	defaults := map[string]graph.Point{}
	if ordered, err := recipe.Order(e.Definition.Steps); err == nil {
		depth := map[string]int{}
		rows := map[int]int{}
		for _, step := range ordered {
			level := 0
			dep := func(id string) { level = max(level, depth[id]+1) }
			for _, id := range step.DependsOn {
				dep(id)
			}
			for _, raw := range []json.RawMessage{step.Args, step.When, step.ForEach} {
				var v any
				_ = json.Unmarshal(raw, &v)
				refs(v, dep)
			}
			depth[step.ID] = level
			defaults[step.ID] = graph.Point{X: 2 + level*30, Y: 1 + rows[level]*8}
			rows[level]++
		}
	}
	g := graph.Model{Selected: old.Selected, Pan: old.Pan}
	for i, s := range e.Definition.Steps {
		p, ok := positions[s.ID]
		if !ok {
			p = graph.Point{X: 2 + (i%3)*30, Y: 1 + (i/3)*8}
			if point, found := defaults[s.ID]; found {
				p = point
			}
		}
		detail := ""
		if s.Review {
			detail = "Review before running"
		}
		g.Nodes = append(g.Nodes, graph.Node{ID: s.ID, Label: s.Tool, Detail: detail, Point: p})
		for _, dep := range s.DependsOn {
			g.Edges = append(g.Edges, graph.Edge{From: dep, To: s.ID})
		}
		for _, raw := range []json.RawMessage{s.Args, s.When, s.ForEach} {
			var v any
			_ = json.Unmarshal(raw, &v)
			refs(v, func(id string) { g.Edges = append(g.Edges, graph.Edge{From: id, To: s.ID, Data: true}) })
		}
	}
	if g.Selected == "" && len(g.Nodes) > 0 {
		g.Selected = g.Nodes[0].ID
	}
	return g
}
func refs(v any, emit func(string)) {
	switch v := v.(type) {
	case map[string]any:
		if id, ok := v["$step"].(string); ok {
			emit(id)
		}
		for _, x := range v {
			refs(x, emit)
		}
	case []any:
		for _, x := range v {
			refs(x, emit)
		}
	}
}
