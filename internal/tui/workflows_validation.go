package tui

import (
	"errors"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/automation"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Error messages are hints for navigation, never an authority to edit recipes.
// Prefer structured schema locations; match only explicit step identifiers.
func workflowErrorLocation(err error, d automation.Definition) (string, string) {
	var validation *jsonschema.ValidationError
	var location []string
	if errors.As(err, &validation) {
		var visit func(*jsonschema.ValidationError)
		visit = func(e *jsonschema.ValidationError) {
			if len(e.InstanceLocation) > len(location) {
				location = e.InstanceLocation
			}
			for _, cause := range e.Causes {
				visit(cause)
			}
		}
		visit(validation)
	}
	id := ""
	for _, step := range d.Steps {
		for _, suffix := range []string{":", " ", "/"} {
			if strings.Contains(err.Error(), "step "+step.ID+suffix) && len(step.ID) > len(id) {
				id = step.ID
			}
		}
	}
	if id != "" {
		// A node argument can itself contain a field named steps. Its schema
		// location is relative to that node, not the recipe's step array.
		return id, workflowErrorPointer(location)
	}
	if len(location) >= 2 && location[0] == "steps" {
		index, e := strconv.Atoi(location[1])
		if e == nil && index >= 0 && index < len(d.Steps) {
			return d.Steps[index].ID, workflowErrorPointer(location[2:])
		}
	}
	return "", ""
}

func workflowErrorPointer(parts []string) string {
	path := ""
	for _, part := range parts {
		path += "/" + strings.NewReplacer("~", "~0", "/", "~1").Replace(part)
	}
	return path
}

func (m *model) workflowValidationRows(err error) []string {
	w := m.workflows
	d := w.editor.Definition
	if w.run != nil {
		d = w.run.Definition
	}
	id, path := workflowErrorLocation(err, d)
	rows := []string{}
	if id != "" {
		if w.run == nil {
			w.graph = m.workflowGraph(w.graph)
		}
		w.graph.Selected = id
		w.inspect = true
		w.inspectorOpen = true
		w.offset = 0
		width, height := m.workflowCanvasSize()
		w.graph.Reveal(width, height)
		rows = append(rows, "Node: "+id)
		if path != "" {
			rows = append(rows, "Value: "+path)
		}
		rows = append(rows, "")
	}
	return append(rows, err.Error())
}
