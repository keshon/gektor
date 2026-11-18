package workflow

import "github.com/keshon/gektor/internal/workflow"

func (m *Model) validationRows(err error) []string {
	d := m.editor.Definition
	if m.run != nil {
		d = m.run.Definition
	}
	diagnostics := workflow.Diagnose(err, d)
	id, path := "", ""
	for _, diagnostic := range diagnostics {
		if diagnostic.StepID != "" && (id == "" || len(diagnostic.Pointer) > len(path)) {
			id, path = diagnostic.StepID, diagnostic.Pointer
		}
	}

	rows := []string{}
	if id != "" {
		if m.run == nil {
			m.graph = m.buildGraph(m.graph)
		}
		m.graph.Selected = id
		m.inspect = true
		m.inspectorOpen = true
		m.offset = 0
		width, height := m.canvasSize()
		m.graph.Reveal(width, height)
		rows = append(rows, "Node: "+id)
		if path != "" {
			rows = append(rows, "Value: "+path)
		}
		rows = append(rows, "")
	}
	return append(rows, err.Error())
}
