package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
)

func seedWorkflowRuns(t *testing.T, m *Model) (older, newer string) {
	t.Helper()
	s, err := store.OpenStore(userdirs.Sub("workflows"), m.launch.Env.WS.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, status := range []string{"completed", "failed"} {
		w, err := s.NewRun()
		if err != nil {
			t.Fatal(err)
		}
		d := workflowRecipe()
		d.Name = "Same recipe"
		err = w.Save(workflow.Run{Definition: d, RecipePath: "recipe.json", Status: status, Started: time.Unix(int64(i+1), 0)})
		id := w.ID()
		w.Close()
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			older = id
		} else {
			newer = id
		}
	}
	return
}

func driveWorkflow(t *testing.T, m *Model) {
	t.Helper()
	commands := []tea.Cmd{m.takeCommands()}
	for count := 0; len(commands) > 0; count++ {
		if count > 100 {
			t.Fatal("workflow effects did not settle")
		}
		cmd := commands[0]
		commands = commands[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			commands = append(commands, batch...)
			continue
		}
		if msg != nil {
			_, next := m.Update(msg)
			commands = append(commands, next)
		}
	}
}

func TestWorkflowRunTableFiltersByStableIDWithoutExecuting(t *testing.T) {
	m := workflowTestModel(t)
	draft := seedWorkflow(t, m, workflowRecipe())
	before := draft.Source()
	older, newer := seedWorkflowRuns(t, m)
	m.workflowCommand("runs")
	if m.chrome.Dialog.Table == nil || m.chrome.Dialog.Table.Model.Selected() != newer {
		t.Fatal("runs are not newest first")
	}
	for _, a := range m.footerActions() {
		if a.Key == tea.KeyEnter && a.Label != "Open run" {
			t.Fatal("footer disagrees with table")
		}
	}
	m.handleKey(tea.KeyPressMsg{Code: '/'})
	m.Update(tea.PasteMsg{Content: "no match"})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chrome.Dialog == nil || len(m.workflows.TakeCommands()) != 0 {
		t.Fatal("empty search activated a run")
	}
	m.closeDialog()
	m.workflowCommand("runs")
	m.handleKey(tea.KeyPressMsg{Code: '/'})
	m.Update(tea.PasteMsg{Content: older})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.chrome.Dialog != nil || len(m.workflows.TakeCommands()) != 1 || m.workflows.State().Busy || m.workflows.State().InspectingRun || draft.Source() != before {
		t.Fatal("browsing started execution or changed the recipe")
	}
}

func TestWorkflowRunTableFitsEveryThemeAndSize(t *testing.T) {
	defer setTheme("dark")
	m := workflowTestModel(t)
	seedWorkflowRuns(t, m)
	for _, theme := range config.ThemeNames() {
		m.applyTheme(theme)
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}, {160, 50}} {
			sizeModelTo(t, m, size[0], size[1])
			m.workflowCommand("runs")
			rows, width, height := m.dialogRows()
			if len(rows)+2 > height {
				t.Fatal("run table overflow", theme, size)
			}
			checkModalWidth(t, m.dialogView(), min(80, width-2))
			text := ansi.Strip(m.dialogView())
			if !strings.Contains(text, "Open run") || !strings.Contains(text, "failed") {
				t.Fatal("table lost action or status", theme, size)
			}
			m.closeDialog()
		}
	}
}

func TestConnectionPreviewFitsResizedForms(t *testing.T) {
	defer setTheme("dark")
	m := workflowTestModel(t)
	d := workflowRecipe()
	second := d.Steps[0]
	second.ID = "second"
	d.Steps = append(d.Steps, second)
	seedWorkflow(t, m, d)
	m.workflowCommand("link")
	if err := m.applyValues(map[string]string{"from": "transform", "to": "second"}); err != nil {
		t.Fatal(err)
	}
	m.closeDialog()
	driveWorkflow(t, m)
	if err := m.applyValues(map[string]string{"from": "transform", "arg": "Argument: input"}); err != nil {
		t.Fatal(err)
	}
	m.closeDialog()
	driveWorkflow(t, m)
	for _, theme := range []string{"light", "dark"} {
		m.applyTheme(theme)
		for _, size := range [][2]int{{120, 30}, {80, 24}, {40, 15}} {
			sizeModelTo(t, m, size[0], size[1])
			rows := strings.Split(ansi.Strip(m.screenContent()), "\n")
			if len(rows) != size[1] {
				t.Fatal("form height overflow", theme, size)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) != size[0] {
					t.Fatal("form width overflow", theme, size, row)
				}
			}
		}
	}
}

func TestWorkflowErrorFooterFitsEveryTheme(t *testing.T) {
	defer setTheme("dark")
	m := workflowTestModel(t)
	d := workflowRecipe()
	d.Steps[0].Tool = "missing_operation"
	seedWorkflow(t, m, d)
	m.workflowCommand("validate")
	driveWorkflow(t, m)
	if m.chrome.Dialog == nil {
		t.Fatal("invalid recipe did not report its validation failure")
	}
	m.closeDialog()
	if m.workflows.State().InspectorOpen {
		m.workflowCommand("inspector")
	}
	for _, theme := range config.ThemeNames() {
		m.applyTheme(theme)
		for _, size := range [][2]int{{40, 20}, {80, 24}, {120, 40}, {160, 50}} {
			sizeModelTo(t, m, size[0], size[1])
			text := ansi.Strip(m.View().Content)
			if !strings.Contains(text, "Error:") {
				t.Fatal("workflow failure missing", theme, size)
			}
			rows := strings.Split(text, "\n")
			if len(rows) != size[1] {
				t.Fatal("notice changed screen height")
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > size[0] {
					t.Fatal("notice overflowed")
				}
			}
		}
	}
}
