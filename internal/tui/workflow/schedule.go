package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/robfig/cron/v3"

	"github.com/keshon/gektor/internal/tui/components/graph"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
)

const workflowSchedulePrefix = "@schedule/"

type workflowScheduleEdit struct {
	Draft    string
	Schedule store.Schedule
	Expected *store.Schedule
}

func (m *Model) selectedSchedule() *store.Schedule {
	for _, s := range m.recipeSchedules() {
		if workflowSchedulePrefix+s.ID == m.graph.Selected {
			return &s
		}
	}
	return nil
}

func (m *Model) recipeSchedules() []store.Schedule {
	if m.config.WS == nil || m.editor.Path == "" {
		return nil
	}
	path, err := m.config.WS.Relative(m.editor.Path)
	if err != nil {
		return nil
	}
	var out []store.Schedule
	for _, s := range m.schedules {
		p, err := m.config.WS.Relative(s.RecipePath)
		if err == nil && filepath.Clean(p) == filepath.Clean(path) {
			out = append(out, s)
		}
	}
	return out
}

// Triggers start the recipe, not individual operations. These edges describe
// that relationship without adding executable steps or dependencies to JSON.
func (m *Model) buildGraph(old graph.Model) graph.Model {
	g := m.editor.Graph(old)
	if m.run != nil {
		return g
	}
	schedules := m.recipeSchedules()
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

func (m *Model) schedule() {
	m.scheduleStart(false)
}

func (m *Model) scheduleStart(create bool) {
	if m.run != nil || m.busy {
		return
	}
	if m.editor.Path == "" || m.editor.Dirty() {
		m.setNotice(pane.NoticeWarning, "Save the recipe before adding or editing a schedule")
		return
	}
	id := ""
	inputs := json.RawMessage(`{}`)
	if s := m.selectedSchedule(); s != nil && !create {
		id = s.ID
		inputs, _ = json.Marshal(s.Inputs)
	} else if schedules := m.recipeSchedules(); len(schedules) > 0 && !create {
		id = schedules[0].ID
		inputs, _ = json.Marshal(schedules[0].Inputs)
		m.graph.Selected = workflowSchedulePrefix + id
	}
	in := workflowInvocation{Action: "schedule", Draft: m.editor.Source(), Mode: "Simple", Inputs: inputs, ScheduleID: id}
	if len(m.editor.Definition.Inputs) == 0 {
		m.scheduleForm(in)
		return
	}
	m.inputForm(in)
}

func (m *Model) scheduleForm(in workflowInvocation) {
	if m.busy || m.run != nil || m.editor.Source() != in.Draft || m.editor.Dirty() || m.editor.Path == "" {
		m.setNotice(pane.NoticeWarning, "Recipe changed; save it and reopen Schedule")
		return
	}
	path, err := m.config.WS.Relative(m.editor.Path)
	if err != nil {
		m.setNotice(pane.NoticeError, err.Error())
		return
	}
	s := store.Schedule{ID: fmt.Sprintf("recipe-%x", sha256.Sum256([]byte(filepath.ToSlash(path))))[:23], RecipePath: path, Cron: "0 * * * *", Timezone: "UTC", Overlap: "skip", Missed: "skip"}
	baseID := s.ID
	for suffix := 2; ; suffix++ {
		used := false
		for _, saved := range m.schedules {
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
		for _, saved := range m.schedules {
			if saved.ID == in.ScheduleID {
				s, expected = saved, &saved
				break
			}
		}
		if expected == nil {
			m.setNotice(pane.NoticeWarning, "Schedule changed; refresh schedules first")
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
	m.openForm(screen.Form{
		Title:      "Schedule trigger",
		Hint:       "Saves this recipe and its input values as a schedule. Keep the workflow service running separately.",
		ApplyLabel: "Save schedule",
		Fields:     fields,
		Choices:    map[string][]string{"timing": {"Every hour", "Daily", "Cron"}, "enabled": {"Yes", "No"}},
	}, editSchedule{
		expected: expected,
		in:       in,
		path:     path,
		s:        s,
	})

}

func (m *Model) scheduleRefresh() tea.Cmd {
	if m.run != nil {
		m.setNotice(pane.NoticeWarning, "Return to the draft before refreshing schedules")
		return nil
	}
	m.busy = true
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	root, base := m.config.WS.Root(), userdirs.Sub("workflows")
	svc, serviceErr := m.service(nil)
	return func() tea.Msg {
		defer cancel()
		if serviceErr != nil {
			return workflowResult{action: "schedule-refresh", err: serviceErr}
		}
		s, err := store.OpenStore(base, root)
		result := workflowResult{action: "schedule-refresh", err: err}
		if err == nil {
			defer s.Close()
			loadScheduleInspection(ctx, s, svc, &result)
		}
		return result
	}
}

func (m *Model) scheduleChange(action, payload string) tea.Cmd {
	var edit workflowScheduleEdit
	if err := json.Unmarshal([]byte(payload), &edit); err != nil || m.run != nil || m.editor.Dirty() || edit.Draft != m.editor.Source() {
		m.setNotice(pane.NoticeWarning, "Recipe changed; reopen Schedule")
		return nil
	}
	path, err := m.config.WS.Relative(m.editor.Path)
	if err != nil || path != edit.Schedule.RecipePath {
		m.setNotice(pane.NoticeWarning, "Recipe path changed; reopen Schedule")
		return nil
	}
	m.busy = true
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	root, base := m.config.WS.Root(), userdirs.Sub("workflows")
	svc, serviceErr := m.service(nil)
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
			loadScheduleInspection(ctx, s, svc, &result)
		}
		return result
	}
}

// Called by commands, never during rendering; source reads use no approval gate.
func loadScheduleInspection(ctx context.Context, db *store.Store, svc service.Service, result *workflowResult) {
	result.schedules, result.err = db.Schedules(ctx)
	if result.err != nil {
		return
	}
	result.workerLeaseHeld, result.err = db.WorkerLeaseHeld()
	if result.err != nil {
		return
	}
	jobs, err := db.Jobs(ctx)
	if err != nil {
		result.err = err
		return
	}
	result.scheduleStatus, result.err = svc.InspectSchedules(ctx, db, result.schedules, jobs)
}

func (m *Model) deleteSchedule(s store.Schedule) {
	draft := m.editor.Source()
	m.openForm(screen.Form{
		Title:       "Remove schedule",
		Hint:        "Stops future triggers. Existing runs and queued jobs are kept.",
		ApplyLabel:  "Remove",
		Fields:      []formField{{Key: "id", Label: "Type schedule ID to remove: " + s.ID}},
		Destructive: true,
	}, removeSchedule{
		draft: draft,
		s:     s,
	})

}

func (m *Model) scheduleDetails(s store.Schedule) []string {
	rows := []string{"Inspect", m.surface.Styles.Accent.Render("Schedule trigger"), ""}
	if m.rawDetails {
		b, _ := json.MarshalIndent(s, "", "  ")
		return append(rows, strings.Split(cleanText(string(b)), "\n")...)
	}
	switch m.inspectorTab {
	case 1:
		rows = append(rows, "Values used for each scheduled run:", "")
		if len(s.Inputs) == 0 {
			rows = append(rows, "No overrides; the saved recipe defaults are used.")
		} else {
			b, _ := json.Marshal(s.Inputs)
			rows = append(rows, workflowValues(b)...)
		}
	case 2:
		rows = append(rows, "Keep the workflow service running:", "gektor workflow serve -workspace \""+m.config.WS.Root()+"\"", "", "The service uses its own permission rules.", "Start it outside GEKTOR if it must outlive the TUI.", "", "If the previous run is still active: "+s.Overlap,
			"If the service missed a scheduled time: "+s.Missed, "", "Scheduled runs are available under Run, Run history.")
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
		if m.workerLeaseHeld {
			rows = append(rows, "", "Worker: queue owned (health not checked)")
		} else {
			rows = append(rows, "", "Worker: no queue owner at last refresh")
		}
		for _, status := range m.scheduleStatus {
			if status.ID != s.ID {
				continue
			}
			rows = append(rows, "Snapshot: "+status.Snapshot.State)
			for _, path := range status.Snapshot.ChangedFiles {
				rows = append(rows, "Changed: "+cleanText(path))
			}
			for _, path := range status.Snapshot.UnavailableFiles {
				rows = append(rows, "Could not check: "+cleanText(path))
			}
			if status.Snapshot.State == "changed" {
				rows = append(rows, "Edit and save the trigger to refresh its snapshot.")
			}
			if status.LatestJob != nil {
				rows = append(rows, "Last job: "+status.LatestJob.Status)
				if status.LatestJob.Error != "" {
					rows = append(rows, "Job error: "+cleanText(status.LatestJob.Error))
				}
				if status.LatestJob.ScheduledFor != nil {
					rows = append(rows, "Scheduled for: "+status.LatestJob.ScheduledFor.Format(time.RFC3339))
				}
			}
			if status.LatestRun != nil {
				run := status.LatestRun
				rows = append(rows, "Last run: "+run.Status, "Run ID: "+run.ID, "Started: "+run.Started.Format(time.RFC3339))
				if run.Outputs.Omitted {
					rows = append(rows, fmt.Sprintf("Outputs: %d bytes; inspect the saved run for details", run.Outputs.Bytes))
				} else if len(run.Outputs.Value) > 0 {
					rows = append(rows, "Outputs:")
					rows = append(rows, workflowValues(run.Outputs.Value)...)
				}
				for _, failure := range run.Errors {
					rows = append(rows, cleanText(failure.ID+": "+failure.Error))
				}
			}
			if status.RunError != "" {
				rows = append(rows, "Run unavailable: "+cleanText(status.RunError))
			}
		}
		rows = append(rows, "Refresh schedules to update these observations.")
		root := s.Source
		var bundle struct {
			Root json.RawMessage `json:"root"`
		}
		if json.Unmarshal(s.Source, &bundle) == nil && bundle.Root != nil {
			root = bundle.Root
		}
		if saved, err := Decode(root); err == nil && (&Editor{Definition: saved}).Source() != m.editor.Source() {
			rows = append(rows, "", "The current draft differs from this schedule snapshot.", "Edit and save the trigger to use the current recipe.")
		}
		rows = append(rows, "", "Enter edits timing and input values.", "Delete removes this trigger.")
	}
	return rows
}

type editSchedule struct {
	expected *store.Schedule
	in       workflowInvocation
	path     string
	s        store.Schedule
}

func (edit editSchedule) apply(m *Model, v map[string]string) error {
	current, err := m.config.WS.Relative(m.editor.Path)
	if err != nil || current != edit.path || m.run != nil || m.busy || m.editor.Dirty() || m.editor.Source() != edit.in.Draft {
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

	_, cronErr := cron.ParseStandard(expression)
	if cronErr != nil {
		return fmt.Errorf("cron: %w", cronErr)
	}

	zone := strings.TrimSpace(v["timezone"])
	_, zoneErr := time.LoadLocation(zone)
	if zoneErr != nil {
		return fmt.Errorf("use a time zone such as Europe/Moscow or UTC")
	}
	if v["enabled"] != "Yes" && v["enabled"] != "No" {
		return fmt.Errorf("choose Yes or No")
	}

	changed := edit.s
	changed.Cron, changed.Timezone, changed.Paused, changed.Source = expression, zone, v["enabled"] == "No", json.RawMessage(edit.in.Draft)
	changed.Inputs = nil
	decodeErr := json.Unmarshal(edit.in.Inputs, &changed.Inputs)
	if decodeErr != nil {
		return decodeErr
	}

	payload, _ := json.Marshal(workflowScheduleEdit{Draft: edit.in.Draft, Schedule: changed, Expected: edit.expected})
	m.queueAction("schedule-save", string(payload))
	return nil
}

type removeSchedule struct {
	draft string
	s     store.Schedule
}

func (edit removeSchedule) apply(m *Model, v map[string]string) error {
	if v["id"] != edit.s.ID {
		return fmt.Errorf("type %s", edit.s.ID)
	}
	payload, _ := json.Marshal(workflowScheduleEdit{Draft: edit.draft, Schedule: edit.s, Expected: &edit.s})
	m.queueAction("schedule-delete", string(payload))
	return nil
}
