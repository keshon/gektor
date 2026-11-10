// Package workflow owns editable recipes; rendering and execution live in adapters.
package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/keshon/gektor/internal/workflow"
)

type Editor struct {
	Definition workflow.Definition
	Path       string
	Saved      string
	undo, redo []string
}

func New() *Editor {
	e := &Editor{Definition: workflow.Definition{Version: 3, Name: "Untitled workflow", Steps: []workflow.Step{}}}
	e.Saved = e.Source()
	return e
}
func Decode(source []byte) (workflow.Definition, error) {
	var d workflow.Definition
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
func (e *Editor) Replace(d workflow.Definition) {
	e.undo = append(e.undo, e.Source())
	if len(e.undo) > 100 {
		e.undo = e.undo[1:]
	}
	e.redo = nil
	b, _ := json.Marshal(d)
	// Decode into a fresh value: reusing the old struct retains omitted fields.
	e.Definition, _ = Decode(b)
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
func (e *Editor) Clone() workflow.Definition { d, _ := Decode([]byte(e.Source())); return d }
func (e *Editor) Link(from, to, arg, pointer string) error {
	c := Connection{From: from, To: to, Field: "depends_on", Pointer: pointer}
	if arg != "" {
		c.Field, c.Path = "args", "/"+escapePointer(arg)
	}
	return e.ChangeConnection(nil, &c)
}
