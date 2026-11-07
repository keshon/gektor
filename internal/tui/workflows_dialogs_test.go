package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/config"
)

func TestWorkflowRunTableFiltersByStableIDWithoutExecuting(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	draft := w.editor.Source()
	runs := []store.RunSummary{{ID: "older", Name: "Same recipe", Status: "completed", Started: time.Unix(1, 0)}, {ID: "newer", Name: "Same recipe", Status: "failed", Started: time.Unix(2, 0)}}
	m.openWorkflowRuns(runs)
	for _, action := range m.footerActions() {
		if action.key == tea.KeyEnter && action.label != "Open run" {
			t.Fatal("hotkey bar does not match the table action")
		}
	}
	if m.dialog.table == nil || m.dialog.table.model.Selected() != "newer" || runs[0].ID != "older" {
		t.Fatal("runs are not newest first or browsing changed the source list")
	}
	m.handleKey(tea.KeyPressMsg{Code: '/'})
	m.Update(tea.PasteMsg{Content: "no match"})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog == nil || len(m.pendingCommands) != 0 {
		t.Fatal("empty search activated a run")
	}
	m.closeDialog()
	m.openWorkflowRuns(runs)
	m.handleKey(tea.KeyPressMsg{Code: '/'})
	m.Update(tea.PasteMsg{Content: "older"})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.dialog != nil || len(m.pendingCommands) != 1 || w.busy || w.run != nil || w.editor.Source() != draft {
		t.Fatal("opening a saved record started execution or changed the draft")
	}
	if action := m.pendingCommands[0]().(workflowAction); action.action != "inspect-run" || action.value != "older" {
		t.Fatal("run identity was confused with its display label", action)
	}
}

func TestWorkflowRunTableFitsEveryThemeAndSize(t *testing.T) {
	defer setTheme("dark")
	m := workflowTestModel(t)
	for _, theme := range config.ThemeNames() {
		m.applyTheme(theme)
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}, {160, 50}} {
			sizeModelTo(t, m, size[0], size[1])
			m.openWorkflowRuns([]store.RunSummary{{ID: "run-id", Name: "文件 recipe with a long name", Status: "failed", Started: time.Unix(1, 0)}})
			rows, width, height := m.dialogRows()
			if len(rows)+2 > height {
				t.Fatal("run table exceeded dialog height", theme, size)
			}
			checkModalWidth(t, m.dialogView(), min(80, width-2))
			if !strings.Contains(ansi.Strip(m.dialogView()), "Open run") {
				t.Fatal("run table lost its explicit action")
			}
			if !strings.Contains(ansi.Strip(m.dialogView()), "failed") {
				t.Fatal("run status was squeezed out by optional metadata")
			}
			m.closeDialog()
		}
	}
}

func TestWorkflowValueRemovalStartsOnCancelAndRequiresExplicitAction(t *testing.T) {
	for _, kind := range []string{"inputs", "outputs"} {
		m := workflowTestModel(t)
		w := m.workflows
		d := workflowRecipe()
		d.Inputs = map[string]automation.Input{"value": {Type: "string"}}
		d.Outputs = map[string]json.RawMessage{"value": json.RawMessage(`"result"`)}
		w.editor.Replace(d)
		before := w.editor.Source()
		open := func() {
			m.workflowParameterEdit(workflowParameterEdit{Kind: kind, Name: "value", Draft: before, Remove: true})
		}
		open()
		f := m.dialog.form
		if !f.Destructive || f.Index != len(f.Fields)+1 || !f.Fields[0].ReadOnly {
			t.Fatal("removal lost context, destructive style or Cancel-first focus")
		}
		m.handleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
		if w.editor.Source() != before || m.dialog == nil {
			t.Fatal("save shortcut performed a removal")
		}
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.dialog != nil || w.editor.Source() != before {
			t.Fatal("Enter on Cancel removed a value")
		}
		open()
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
		m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.dialog != nil || w.editor.Source() == before {
			t.Fatal("explicit removal did not apply")
		}
		w.editor.Undo()
		if w.editor.Source() != before {
			t.Fatal("removal lost undo support")
		}
	}
}

func TestWorkflowJSONInputsNameTheirActualAction(t *testing.T) {
	m := workflowTestModel(t)
	for _, item := range []struct{ action, label string }{{"run", "Run"}, {"validate", "Check"}, {"schedule", "Next"}} {
		m.workflowInputForm(workflowInvocation{Action: item.action, Mode: "JSON", Draft: m.workflows.editor.Source()})
		if m.dialog.form.ApplyLabel != item.label || !strings.Contains(m.dialog.title, "JSON") {
			t.Fatal("JSON form does not name what its primary action does")
		}
		m.closeDialog()
	}
}
