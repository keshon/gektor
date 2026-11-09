package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/automation/service"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/workspace"
)

type Status struct {
	WS          *workspace.Workspace
	StoreDir    string
	CheckSource func(context.Context, string, json.RawMessage) error
}

func (Status) Name() string { return "workflow_status" }
func (Status) Description() string {
	return "Read workflow worker ownership, schedule settings, snapshot changes, latest declared outputs, recent jobs and run IDs in this workspace. Filter by schedule_id to trace scheduled runs, then use workflow_inspect for outputs. A held worker lease proves ownership, not health or success. Does not start a service, reconcile jobs or execute recipes."
}
func (Status) ReadOnly() bool       { return true }
func (Status) Mode() agent.ToolMode { return agent.Concurrent }
func (Status) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"schedule_id":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":50}},"additionalProperties":false}`)
}
func (t Status) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.WS == nil {
		return "", fmt.Errorf("workspace unavailable")
	}
	if err := agent.NewRegistry(t).ValidateCall(t.Name(), args); err != nil {
		return "", err
	}
	var in struct {
		Schedule string `json:"schedule_id"`
		Limit    int    `json:"limit"`
	}
	if err := decode(args, &in); err != nil {
		return "", err
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	db, err := store.OpenStore(t.StoreDir, t.WS.Root())
	if err != nil {
		return "", err
	}
	defer db.Close()
	held, err := db.WorkerLeaseHeld()
	if err != nil {
		return "", err
	}
	jobs, err := db.Jobs(ctx)
	if err != nil {
		return "", err
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Created.After(jobs[j].Created) })
	out := []service.JobInspection{}
	linked := map[string]bool{}
	for _, j := range jobs {
		if in.Schedule != "" && j.Schedule != in.Schedule {
			continue
		}
		if len(out) >= in.Limit {
			break
		}
		out = append(out, service.InspectJob(j))
		linked[j.RunID] = true
	}
	schedules, err := db.Schedules(ctx)
	if err != nil {
		return "", err
	}
	selected := []store.Schedule{}
	for _, schedule := range schedules {
		if in.Schedule != "" && schedule.ID != in.Schedule {
			continue
		}
		if len(selected) >= in.Limit {
			break
		}
		selected = append(selected, schedule)
	}
	svc := service.Service{WS: t.WS, Authorize: t.CheckSource}
	summaries, err := svc.InspectSchedules(ctx, db, selected, jobs)
	if err != nil {
		return "", err
	}
	runs, err := db.Runs()
	if err != nil {
		return "", err
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Started.After(runs[j].Started) })
	type run struct {
		ID      string    `json:"run_id"`
		Name    string    `json:"name"`
		Recipe  string    `json:"recipe_path"`
		Status  string    `json:"status"`
		Started time.Time `json:"started"`
		Error   string    `json:"error,omitempty"`
	}
	recent := []run{}
	for _, r := range runs {
		if in.Schedule != "" && !linked[r.ID] {
			continue
		}
		if len(recent) >= in.Limit {
			break
		}
		recent = append(recent, run{r.ID, r.Name, r.RecipePath, r.Status, r.Started, r.Error})
	}
	b, err := json.Marshal(struct {
		WorkerLeaseHeld bool                         `json:"worker_lease_held"`
		Jobs            []service.JobInspection      `json:"jobs"`
		Runs            []run                        `json:"runs"`
		Schedules       []service.ScheduleInspection `json:"schedules"`
		Note            string                       `json:"note"`
	}{held, out, recent, summaries, "Worker ownership is not a health check. Jobs are saved observations; inspect run outputs before claiming success. No worker lease means no workflow service currently owns this workspace queue."})
	return string(b), err
}
