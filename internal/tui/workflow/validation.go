package workflow

import (
	"errors"
	"path/filepath"

	"github.com/keshon/gektor/internal/workflow"
)

func (m *Model) validationRows(err error) []string {
	d, sourcePath := m.editor.Definition, m.editor.Path
	if sourcePath == "" {
		sourcePath = "recipe.json"
	}
	if m.run != nil {
		d, sourcePath = m.run.Definition, m.run.RecipePath
	}

	var sourceError *workflow.RecipeError
	foreignRecipe := errors.As(err, &sourceError) && filepath.Clean(sourceError.Path) != filepath.Clean(sourcePath)
	if foreignRecipe {
		d = workflow.Definition{}
	}

	diagnostics := workflow.Diagnose(err, d)
	id, path := "", ""
	for _, diagnostic := range diagnostics {
		if !foreignRecipe && diagnostic.StepID != "" && (id == "" || len(diagnostic.Pointer) > len(path)) {
			id, path = diagnostic.StepID, diagnostic.Pointer
		}
	}

	rows := []string{}
	if sourceError != nil {
		rows = append(rows, "Recipe: "+sourceError.Path, "")
	}
	if id != "" {
		if m.run == nil {
			m.graph = m.buildGraph(m.graph)
		}
		m.graph.Selected = id
		m.inspect = true
		m.inspectorOpen = true
		m.setInspectorTab(0)
		width, height := m.canvasSize()
		m.graph.Reveal(width, height)
		rows = append(rows, "Node: "+id)
		if path != "" {
			rows = append(rows, "Value: "+path)
		}
		if m.run == nil {
			rows = append(rows, "Close this dialog, then press Enter to edit the selected node.")
		}
		rows = append(rows, "")
	}
	return append(rows, err.Error())
}
