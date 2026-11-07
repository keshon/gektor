package tui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/gektor"
	"github.com/keshon/gektor/internal/tui/components/graph"
	editor "github.com/keshon/gektor/internal/tui/workflow"
	"github.com/robfig/cron/v3"
)

const workflowSchedulePrefix = "@schedule/"

type workflowScheduleEdit struct {
	Draft    string
	Schedule store.Schedule
	Expected *store.Schedule
}

func (m *model) workflowSelectedSchedule() *store.Schedule {
	for _, s := range m.workflowRecipeSchedules() {
		if workflowSchedulePrefix+s.ID == m.workflows.graph.Selected {
			return &s
		}
	}
	return nil
}

func (m *model) workflowRecipeSchedules() []store.Schedule {
	if m.baseEnv.WS == nil || m.workflows.editor.Path == "" {
		return nil
	}
	path, err := m.baseEnv.WS.Relative(m.workflows.editor.Path)
	if err != nil {
		return nil
	}
	var out []store.Schedule
	for _, s := range m.workflows.schedules {
		p, err := m.baseEnv.WS.Relative(s.RecipePath)
		if err == nil && filepath.Clean(p) == filepath.Clean(path) {
			out = append(out, s)
		}
	}
	return out
}

// Triggers start the recipe, not individual operations. These edges describe
// that relationship without adding executable steps or dependencies to JSON.
func (m *model) workflowGraph(old graph.Model) graph.Model {
	g := m.workflows.editor.Graph(old)
	if m.workflows.run != nil {
		return g
	}
	schedules := m.workflowRecipeSchedules()
	if len(schedules) == 0 {
		return g
	}
	positions := map[string]graph.Point{}
	for _, n := range old.Nodes {
		if strings.HasPrefix(n.ID, workflowSchedulePrefix) {
			positions[n.ID] = n.Point
		}
	}
	if len(positions) == 0 {
		for i := range g.Nodes {
			g.Nodes[i].X += graph.NodeWidth + 10
		}
	}
	hasIncoming := map[string]bool{}
	for _, edge := range g.Edges {
		hasIncoming[edge.To] = true
	}
	steps := append([]graph.Node(nil), g.Nodes...)
	var triggers []graph.Node
	for i, s := range schedules {
		id := workflowSchedulePrefix + s.ID
		point, ok := positions[id]
		if !ok {
			point = graph.Point{X: 2, Y: 1 + i*11}
		}
		status := "Enabled / service required"
		if s.Paused {
			status = "Paused"
		}
		triggers = append(triggers, graph.Node{ID: id, Title: "Schedule", Label: scheduleTiming(s.Cron), Detail: status,
			Inputs: []graph.Port{}, Outputs: []graph.Port{{Key: "@order", Label: "start"}}, Point: point})
		for _, step := range steps {
			if !hasIncoming[step.ID] {
				g.Edges = append(g.Edges, graph.Edge{From: id, To: step.ID, FromPort: "@order", ToPort: "@order"})
			}
		}
	}
	g.Nodes = append(triggers, g.Nodes...)
	return g
}

func scheduleTiming(expression string) string {
	if expression == "0 * * * *" {
		return "Every hour"
	}
	fields := strings.Fields(expression)
	if len(fields) == 5 && fields[2] == "*" && fields[3] == "*" && fields[4] == "*" {
		hour, hErr := strconv.Atoi(fields[1])
		minute, mErr := strconv.Atoi(fields[0])
		if hErr == nil && mErr == nil && hour >= 0 && hour < 24 && minute >= 0 && minute < 60 {
			return fmt.Sprintf("Daily at %02d:%02d", hour, minute)
		}
	}
	return "Cron: " + expression
}

func (m *model) workflowSchedule() {
	m.workflowScheduleStart(false)
}

func (m *model) workflowScheduleStart(create bool) {
	w := m.workflows
	if w.run != nil || w.busy {
		return
	}
	if w.editor.Path == "" || w.editor.Dirty() {
		w.setNotice(pane.NoticeWarning, "Save the recipe before adding or editing a schedule")
		return
	}
	id := ""
	inputs := json.RawMessage(`{}`)
	if s := m.workflowSelectedSchedule(); s != nil && !create {
		id = s.ID
		inputs, _ = json.Marshal(s.Inputs)
	} else if schedules := m.workflowRecipeSchedules(); len(schedules) > 0 && !create {
		id = schedules[0].ID
		inputs, _ = json.Marshal(schedules[0].Inputs)
		w.graph.Selected = workflowSchedulePrefix + id
	}
	in := workflowInvocation{Action: "schedule", Draft: w.editor.Source(), Mode: "Simple", Inputs: inputs, ScheduleID: id}
	if len(w.editor.Definition.Inputs) == 0 {
		m.workflowScheduleForm(in)
		return
	}
	m.workflowInputForm(in)
}

func (m *model) workflowScheduleForm(in workflowInvocation) {
	w := m.workflows
	if w.busy || w.run != nil || w.editor.Source() != in.Draft || w.editor.Dirty() || w.editor.Path == "" {
		w.setNotice(pane.NoticeWarning, "Recipe changed; save it and reopen Schedule")
		return
	}
	path, err := m.baseEnv.WS.Relative(w.editor.Path)
	if err != nil {
		w.setNotice(pane.NoticeError, err.Error())
		return
	}
	s := store.Schedule{ID: fmt.Sprintf("recipe-%x", sha256.Sum256([]byte(filepath.ToSlash(path))))[:23], RecipePath: path, Cron: "0 * * * *", Timezone: "UTC", Overlap: "skip", Missed: "skip"}
	baseID := s.ID
	for suffix := 2; ; suffix++ {
		used := false
		for _, saved := range w.schedules {
			if saved.ID == s.ID {
				used = true
				break
			}
		}
		if !used {
			break
		}
		s.ID = fmt.Sprintf("%s-%d", baseID, suffix)
	}
	var expected *store.Schedule
	if in.ScheduleID != "" {
		for _, saved := range w.schedules {
			if saved.ID == in.ScheduleID {
				s, expected = saved, &saved
				break
			}
		}
		if expected == nil {
			w.setNotice(pane.NoticeWarning, "Schedule changed; refresh schedules first")
			return
		}
	}
	mode, clock := "Cron", "09:00"
	if s.Cron == "0 * * * *" {
		mode = "Every hour"
	} else if strings.HasPrefix(scheduleTiming(s.Cron), "Daily at ") {
		mode, clock = "Daily", strings.TrimPrefix(scheduleTiming(s.Cron), "Daily at ")
	}
	enabled := "Yes"
	if s.Paused {
		enabled = "No"
	}
	fields := []formField{{Key: "timing", Label: "When", Value: mode}, {Key: "time", Label: "Time (daily only, HH:MM)", Value: clock},
		{Key: "cron", Label: "Cron (Cron only, minute hour day month weekday)", Value: s.Cron},
		{Key: "timezone", Label: "Time zone", Value: s.Timezone}, {Key: "enabled", Label: "Enabled", Value: enabled}}
	m.openForm("Schedule trigger", "Saves this recipe and its input values as a schedule. Keep the workflow service running; closing the TUI is fine.", "Save schedule", fields, func(v map[string]string) error {
		current, err := m.baseEnv.WS.Relative(w.editor.Path)
		if err != nil || current != path || w.run != nil || w.busy || w.editor.Dirty() || w.editor.Source() != in.Draft {
			return fmt.Errorf("recipe changed; reopen Schedule")
		}
		expression := strings.TrimSpace(v["cron"])
		switch v["timing"] {
		case "Every hour":
			expression = "0 * * * *"
		case "Daily":
			clock, err := time.Parse("15:04", v["time"])
			if err != nil {
				return fmt.Errorf("enter a time such as 09:00")
			}
			expression = fmt.Sprintf("%d %d * * *", clock.Minute(), clock.Hour())
		case "Cron":
		default:
			return fmt.Errorf("choose Every hour, Daily or Cron")
		}
		if _, err := cron.ParseStandard(expression); err != nil {
			return fmt.Errorf("cron: %w", err)
		}
		zone := strings.TrimSpace(v["timezone"])
		if _, err := time.LoadLocation(zone); err != nil {
			return fmt.Errorf("use a time zone such as Europe/Moscow or UTC")
		}
		if v["enabled"] != "Yes" && v["enabled"] != "No" {
			return fmt.Errorf("choose Yes or No")
		}
		changed := s
		changed.Cron, changed.Timezone, changed.Paused, changed.Source = expression, zone, v["enabled"] == "No", json.RawMessage(in.Draft)
		changed.Inputs = nil
		if err := json.Unmarshal(in.Inputs, &changed.Inputs); err != nil {
			return err
		}
		payload, _ := json.Marshal(workflowScheduleEdit{Draft: in.Draft, Schedule: changed, Expected: expected})
		m.queueWorkflow("schedule-save", string(payload))
		return nil
	})
	m.dialog.form.Choices = map[string][]string{"timing": {"Every hour", "Daily", "Cron"}, "enabled": {"Yes", "No"}}
}

func (m *model) workflowScheduleRefresh() tea.Cmd {
	w := m.workflows
	if w.run != nil {
		w.setNotice(pane.NoticeWarning, "Return to the draft before refreshing schedules")
		return nil
	}
	w.busy = true
	ctx, cancel := context.WithCancel(m.base)
	w.cancel = cancel
	root, base := m.wsRoot, gektor.Sub("workflows")
	return func() tea.Msg {
		defer cancel()
		s, err := store.OpenStore(base, root)
		result := workflowResult{action: "schedule-refresh", err: err}
		if err == nil {
			defer s.Close()
			result.schedules, result.err = s.Schedules(ctx)
		}
		return result
	}
}

func (m *model) workflowScheduleChange(action, payload string) tea.Cmd {
	w := m.workflows
	var edit workflowScheduleEdit
	if err := json.Unmarshal([]byte(payload), &edit); err != nil || w.run != nil || w.editor.Dirty() || edit.Draft != w.editor.Source() {
		w.setNotice(pane.NoticeWarning, "Recipe changed; reopen Schedule")
		return nil
	}
	path, err := m.baseEnv.WS.Relative(w.editor.Path)
	if err != nil || path != edit.Schedule.RecipePath {
		w.setNotice(pane.NoticeWarning, "Recipe path changed; reopen Schedule")
		return nil
	}
	w.busy = true
	ctx, cancel := context.WithCancel(m.base)
	w.cancel = cancel
	root, base := m.wsRoot, gektor.Sub("workflows")
	svc, serviceErr := m.workflowService(nil)
	return func() tea.Msg {
		defer cancel()
		result := workflowResult{action: action, err: serviceErr, scheduleID: edit.Schedule.ID}
		if result.err != nil {
			return result
		}
		if action == "schedule-save" {
			svc.ValidateOnly = true
			edit.Schedule.Source, result.err = svc.FreezeRecipe(ctx, edit.Schedule.Source, edit.Schedule.RecipePath)
			if result.err != nil {
				return result
			}
			_, result.err = svc.RunSource(ctx, edit.Schedule.Source, edit.Schedule.RecipePath, edit.Schedule.Inputs)
			if result.err != nil {
				return result
			}
		}
		s, err := store.OpenStore(base, root)
		if err != nil {
			result.err = err
			return result
		}
		defer s.Close()
		switch {
		case action == "schedule-delete":
			if edit.Expected == nil {
				result.err = fmt.Errorf("missing schedule revision")
			} else {
				result.err = s.DeleteScheduleIfUnchanged(ctx, *edit.Expected)
			}
		case edit.Expected != nil:
			result.err = s.UpdateSchedule(ctx, edit.Schedule, *edit.Expected)
		default:
			result.err = s.PutSchedule(ctx, edit.Schedule)
		}
		if result.err == nil {
			result.schedules, result.err = s.Schedules(ctx)
		}
		return result
	}
}

func (m *model) workflowDeleteSchedule(s store.Schedule) {
	draft := m.workflows.editor.Source()
	m.openForm("Remove schedule", "Stops future triggers. Existing runs and queued jobs are kept.", "Remove", []formField{{Key: "id", Label: "Type schedule ID to remove: " + s.ID}}, func(v map[string]string) error {
		if v["id"] != s.ID {
			return fmt.Errorf("type %s", s.ID)
		}
		payload, _ := json.Marshal(workflowScheduleEdit{Draft: draft, Schedule: s, Expected: &s})
		m.queueWorkflow("schedule-delete", string(payload))
		return nil
	})
}

func (m *model) workflowScheduleDetails(s store.Schedule) []string {
	w := m.workflows
	rows := []string{"Inspect", m.styles.hunk.Render("Schedule trigger"), ""}
	if w.rawDetails {
		b, _ := json.MarshalIndent(s, "", "  ")
		return append(rows, strings.Split(cleanText(string(b)), "\n")...)
	}
	switch w.inspectorTab {
	case 1:
		rows = append(rows, "Values used for each scheduled run:", "")
		if len(s.Inputs) == 0 {
			rows = append(rows, "No overrides; the saved recipe defaults are used.")
		} else {
			b, _ := json.Marshal(s.Inputs)
			rows = append(rows, workflowValues(b)...)
		}
	case 2:
		rows = append(rows, "Keep the workflow service running:", "gektor workflow serve -workspace \""+m.wsRoot+"\"", "", "The service uses its own permission rules.", "Closing the TUI does not stop the service.", "", "If the previous run is still active: "+s.Overlap,
			"If the service missed a scheduled time: "+s.Missed, "", "Scheduled runs are available under Run, Saved runs.")
	default:
		rows = append(rows, scheduleTiming(s.Cron), "Time zone: "+s.Timezone)
		if s.Paused {
			rows = append(rows, "Status: Paused")
		} else {
			zone, _ := time.LoadLocation(s.Timezone)
			if zone == nil {
				zone = time.UTC
			}
			rows = append(rows, "Status: Enabled", "Next due: "+s.Next.In(zone).Format("2006-01-02 15:04 MST"))
		}
		rows = append(rows, "", "Starts the whole recipe.", "The workflow service must be running.", "", "Schedule ID: "+s.ID, "Cron: "+s.Cron)
		root := s.Source
		var bundle struct {
			Root json.RawMessage `json:"root"`
		}
		if json.Unmarshal(s.Source, &bundle) == nil && bundle.Root != nil {
			root = bundle.Root
		}
		if saved, err := editor.Decode(root); err == nil && (&editor.Editor{Definition: saved}).Source() != w.editor.Source() {
			rows = append(rows, "", "This schedule uses an older recipe snapshot.", "Edit and save the trigger to use the current recipe.")
		}
		rows = append(rows, "", "Enter edits timing and input values.", "Delete removes this trigger.")
	}
	return rows
}
