package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
)

func scheduleFormValues(m *model) map[string]string {
	v := map[string]string{}
	for _, field := range m.dialog.form.Fields {
		v[field.Key] = field.Value
	}
	return v
}

func applyScheduleAction(t *testing.T, m *model) {
	t.Helper()
	a := m.workflows.commands[len(m.workflows.commands)-1]().(workflowAction)
	cmd := m.workflows.action(a)
	if cmd == nil {
		t.Fatal("no schedule operation", a.action, m.workflows.notice)
	}
	r := cmd().(workflowResult)
	if r.err != nil {
		t.Fatal(r.err)
	}
	m.workflows.acceptResult(r)
}

func TestAddScheduleCreatesSecondTriggerAndSelectsIt(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	w.editor.Path = "recipe.json"
	w.editor.Saved = w.editor.Source()
	if err := os.WriteFile(filepath.Join(m.wsRoot, "recipe.json"), []byte(w.editor.Source()), 0600); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := 0; i < 2; i++ {
		m.workflows.scheduleStart(true)
		if m.dialog == nil || m.dialog.form == nil {
			t.Fatal("missing schedule form")
		}
		if err := m.dialog.form.apply(scheduleFormValues(m)); err != nil {
			t.Fatal(err)
		}
		applyScheduleAction(t, m)
		selected := m.workflows.selectedSchedule()
		if selected == nil {
			t.Fatal("new trigger not selected")
		}
		ids = append(ids, selected.ID)
	}
	if len(w.schedules) != 2 || ids[0] == ids[1] {
		t.Fatal("add edited existing trigger", ids, w.schedules)
	}
}

func TestScheduleTriggerRunsSavedInputsAndCanPauseAndRemove(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	d := workflowRecipe()
	d.Inputs = map[string]workflow.Input{"count": {Type: "integer", Default: json.RawMessage(`4`)}}
	d.Steps[0].Args = json.RawMessage(`{"code":"return input + 1;","input":{"$input":"count"}}`)
	w.editor.Replace(d)
	w.editor.Path = "recipe.json"
	w.editor.Saved = w.editor.Source()
	if err := os.WriteFile(filepath.Join(m.wsRoot, "recipe.json"), []byte(w.editor.Source()), 0600); err != nil {
		t.Fatal(err)
	}
	before := w.editor.Source()
	m.workflowCommand("schedule")
	if m.dialog == nil || !strings.Contains(m.dialog.title, "input") {
		t.Fatal("missing typed schedule inputs")
	}
	values := scheduleFormValues(m)
	values["value:count"] = "7"
	if err := m.dialog.form.apply(values); err != nil {
		t.Fatal(err)
	}
	m.workflows.action(m.workflows.commands[len(m.workflows.commands)-1]().(workflowAction))
	values = scheduleFormValues(m)
	values["timing"], values["time"], values["timezone"] = "Daily", "07:15", "Europe/Moscow"
	if err := m.dialog.form.apply(values); err != nil {
		t.Fatal(err)
	}
	applyScheduleAction(t, m)
	if len(w.schedules) != 1 || w.schedules[0].Cron != "15 7 * * *" || string(w.schedules[0].Inputs["count"]) != "7" {
		t.Fatal(w.schedules)
	}
	scheduled := w.schedules[0]
	if w.editor.Source() != before || strings.Contains(string(scheduled.Source), workflowSchedulePrefix) {
		t.Fatal("trigger leaked into executable steps")
	}
	w.graph = m.workflows.buildGraph(w.graph)
	m.workflows.ensureSelection()
	if m.workflows.selectedSchedule() == nil || len(w.graph.Nodes) != 2 || len(w.graph.Edges) != 1 {
		t.Fatal("missing trigger selection or edge", w.graph)
	}
	if !strings.Contains(ansi.Strip(strings.Join(m.workflows.detailRows(), "\n")), "Next due:") {
		t.Fatal("next due missing")
	}
	// The real queue and execution service consume the trigger created through the TUI.
	s, err := store.OpenStore(userdirs.Sub("workflows"), m.wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Tick(t.Context(), scheduled.Next); err != nil {
		t.Fatal(err)
	}
	job, ok, err := s.Claim(t.Context())
	if err != nil || !ok || string(job.Inputs["count"]) != "7" {
		t.Fatal(job, ok, err)
	}
	m.workflows.config.Policy = permission.Policy{Rules: []permission.Rule{{Tool: "workflow_run", Pattern: "*", Effect: permission.Allow}, {Tool: "script_transform", Pattern: "*", Effect: permission.Allow}}}
	svc, err := m.workflows.service(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ExecuteJob(t.Context(), s, job); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.Jobs(t.Context())
	if err != nil || len(jobs) != 1 || jobs[0].Status != "completed" {
		t.Fatal("scheduled run failed", jobs, err)
	}
	// Reopen through the same refresh used for saved recipes, preserving its card position.
	w.graph.Nodes[0].Point = graph.Point{X: 4, Y: 12}
	m.workflows.persistLayout()
	r := m.workflows.scheduleRefresh()().(workflowResult)
	if r.err != nil {
		t.Fatal(r.err)
	}
	m.workflows.acceptResult(r)
	details := strings.Join(m.workflows.scheduleDetails(w.schedules[0]), "\n")
	if !strings.Contains(details, "Last job: completed") || !strings.Contains(details, "Last run: completed") || !strings.Contains(details, "Scheduled for:") {
		t.Fatal(details)
	}
	if w.graph.Nodes[0].Point != (graph.Point{X: 4, Y: 12}) {
		t.Fatal("trigger position lost", w.graph.Nodes)
	}
	m.workflowCommand("schedule")
	values = scheduleFormValues(m)
	if values["value:count"] != "7" {
		t.Fatal("schedule inputs were not restored", values)
	}
	values["value:count"] = "8"
	if err := m.dialog.form.apply(values); err != nil {
		t.Fatal(err)
	}
	m.workflows.action(m.workflows.commands[len(m.workflows.commands)-1]().(workflowAction))
	values = scheduleFormValues(m)
	values["enabled"] = "No"
	if err := m.dialog.form.apply(values); err != nil {
		t.Fatal(err)
	}
	if string(w.schedules[0].Inputs["count"]) != "7" {
		t.Fatal("form submission mutated the saved trigger before storage")
	}
	applyScheduleAction(t, m)
	if !w.schedules[0].Paused || string(w.schedules[0].Inputs["count"]) != "8" {
		t.Fatal("schedule not paused")
	}
	if err := s.Tick(t.Context(), time.Now().Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	jobs, _ = s.Jobs(t.Context())
	if len(jobs) != 1 {
		t.Fatal("paused schedule queued another run")
	}
	m.workflowCommand("delete")
	if err := m.dialog.form.apply(map[string]string{"id": scheduled.ID}); err != nil {
		t.Fatal(err)
	}
	applyScheduleAction(t, m)
	if len(w.schedules) != 0 || len(w.graph.Nodes) != 1 {
		t.Fatal("trigger not removed")
	}
	jobs, _ = s.Jobs(t.Context())
	if len(jobs) != 1 {
		t.Fatal("removing schedule deleted job history")
	}
}

func TestScheduleTriggerGuardsAndTimingValidation(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	m.workflowCommand("schedule")
	if m.dialog != nil || !strings.Contains(w.notice, "Save the recipe") {
		t.Fatal("unsaved recipe scheduled")
	}
	w.editor.Path, w.editor.Saved = "recipe.json", w.editor.Source()
	m.workflowCommand("schedule")
	values := scheduleFormValues(m)
	for _, pair := range [][2]string{{"cron", "garbage"}, {"timezone", "not/a/zone"}, {"time", "25:60"}} {
		bad := scheduleFormValues(m)
		bad[pair[0]] = pair[1]
		if pair[0] == "cron" {
			bad["timing"] = "Cron"
		}
		if pair[0] == "time" {
			bad["timing"] = "Daily"
		}
		if err := m.dialog.form.apply(bad); err == nil {
			t.Fatal("invalid setting accepted", pair)
		}
	}
	if err := m.dialog.form.apply(values); err != nil {
		t.Fatal(err)
	}
	w.editor.Definition.Name = "changed while form open"
	if cmd := m.workflows.action(m.workflows.commands[len(m.workflows.commands)-1]().(workflowAction)); cmd != nil {
		t.Fatal("stale schedule executed")
	}
	m.closeDialog()
	w.run = &workflow.Run{Definition: workflowRecipe()}
	m.workflowCommand("schedule")
	if m.dialog != nil {
		t.Fatal("frozen run offered schedule editing")
	}
}

func TestScheduleCardsFollowRecipeAndDoNotAccumulateOnRedraw(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	w.editor.Replace(workflowRecipe())
	w.editor.Path = "recipe.json"
	w.schedules = []store.Schedule{
		{ID: "from-cli", RecipePath: "./recipe.json", Cron: "0 * * * *", Timezone: "UTC"},
		{ID: "other", RecipePath: "other.json", Cron: "0 9 * * *", Timezone: "UTC"},
	}
	w.graph = m.workflows.buildGraph(graph.Model{})
	position := w.graph.Nodes[1].Point
	w.graph.Selected = workflowSchedulePrefix + "from-cli"
	for i := 0; i < 3; i++ {
		w.graph = m.workflows.buildGraph(w.graph)
		m.workflows.ensureSelection()
		if len(w.graph.Nodes) != 2 || len(w.graph.Edges) != 1 || w.graph.Nodes[1].Point != position || m.workflows.selectedSchedule() == nil {
			t.Fatal("redraw duplicated a trigger or moved the recipe", w.graph)
		}
	}
	w.editor.Path = "other.json"
	m.workflows.ensureSelection()
	if m.workflows.selectedSchedule() != nil {
		t.Fatal("selection retained a different recipe's trigger")
	}
	w.graph = m.workflows.buildGraph(graph.Model{})
	if len(w.graph.Nodes) != 2 || w.graph.Nodes[0].ID != workflowSchedulePrefix+"other" {
		t.Fatal("CLI schedule not attached to its recipe", w.graph)
	}
	w.run = &workflow.Run{Definition: workflowRecipe()}
	w.graph = m.workflows.buildGraph(graph.Model{})
	if len(w.graph.Nodes) != 1 {
		t.Fatal("live trigger leaked into a frozen run")
	}
}

func TestScheduleFreezesCalledRecipe(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	child := `{"version":3,"name":"child-original","steps":[{"id":"calc","tool":"script_transform","args":{"code":"return 1;","input":1}}],"outputs":{"value":{"$step":"calc"}}}`
	if err := m.baseEnv.WS.WriteFile("child.json", []byte(child), 0600); err != nil {
		t.Fatal(err)
	}
	d := workflowRecipe()
	d.Steps[0].Tool = "workflow_call"
	d.Steps[0].Args = json.RawMessage(`{"recipe_path":"child.json","inputs":{}}`)
	w.editor.Replace(d)
	w.editor.Path, w.editor.Saved = "parent.json", w.editor.Source()
	if err := m.baseEnv.WS.WriteFile("parent.json", []byte(w.editor.Source()), 0600); err != nil {
		t.Fatal(err)
	}
	m.workflowCommand("schedule")
	values := scheduleFormValues(m)
	values["enabled"] = "No"
	if err := m.dialog.form.apply(values); err != nil {
		t.Fatal(err)
	}
	applyScheduleAction(t, m)
	if err := m.baseEnv.WS.WriteFile("child.json", []byte(strings.Replace(child, "child-original", "child-changed", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if len(w.schedules) != 1 || !strings.Contains(string(w.schedules[0].Source), "child-original") || strings.Contains(string(w.schedules[0].Source), "child-changed") {
		t.Fatal("called recipe was not frozen", w.schedules)
	}
	w.graph.Selected = workflowSchedulePrefix + w.schedules[0].ID
	r := m.workflows.scheduleRefresh()().(workflowResult)
	if r.err != nil {
		t.Fatal(r.err)
	}
	m.workflows.acceptResult(r)
	details := strings.Join(m.workflows.scheduleDetails(w.schedules[0]), "\n")
	if !strings.Contains(details, "Snapshot: changed") || !strings.Contains(details, "Changed: child.json") || strings.Contains(details, "current draft differs") {
		t.Fatal(details)
	}
}
