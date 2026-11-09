package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/automation"
	workflownodes "github.com/keshon/gektor/internal/automation/nodes/workflow"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestSnapshotInspectionIncludesChildrenAndDoesNotRefresh(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	root := json.RawMessage(`{"version":3,"name":"Parent","steps":[]}`)
	child := json.RawMessage(`{"version":3,"name":"Child","steps":[]}`)
	bundle, _ := json.Marshal(recipeBundle{Root: root, Recipes: map[string]json.RawMessage{"child.json": child}})
	saved := store.Schedule{RecipePath: "parent.json", Source: bundle}
	if err := ws.WriteFile("parent.json", root, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("child.json", child, 0600); err != nil {
		t.Fatal(err)
	}
	svc := Service{WS: ws, Authorize: func(context.Context, string, json.RawMessage) error { return nil }}
	if got := svc.InspectSnapshot(t.Context(), saved); got.State != "unchanged" {
		t.Fatal(got)
	}
	// Formatting and object key order do not create a false drift warning.
	if err := ws.WriteFile("child.json", []byte("{\n \"steps\": [], \"name\": \"Child\", \"version\": 3}"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := svc.InspectSnapshot(t.Context(), saved); got.State != "unchanged" {
		t.Fatal(got)
	}
	if err := ws.WriteFile("child.json", []byte(strings.Replace(string(child), "Child", "Changed", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if got := svc.InspectSnapshot(t.Context(), saved); got.State != "changed" || strings.Join(got.ChangedFiles, ",") != "child.json" {
		t.Fatal(got)
	}
	if string(saved.Source) != string(bundle) {
		t.Fatal("inspection refreshed snapshot")
	}
	svc.Authorize = func(_ context.Context, _ string, args json.RawMessage) error {
		if strings.Contains(string(args), "child.json") {
			return fmt.Errorf("denied")
		}
		return nil
	}
	if got := svc.InspectSnapshot(t.Context(), saved); got.State != "unavailable" || strings.Join(got.UnavailableFiles, ",") != "child.json" {
		t.Fatal(got)
	}
	// Old snapshots without an authorization callback must not read workspace sources.
	svc.Authorize = nil
	if got := svc.InspectSnapshot(t.Context(), saved); got.State != "unavailable" {
		t.Fatal(got)
	}
}

func TestScheduledTimeSurvivesExecutionAndReview(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	base := t.TempDir()
	db, err := store.OpenStore(base, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	nodes := append(workflownodes.Definitions(), automation.Node{Name: "echo", Version: 1, InputSchema: json.RawMessage(`{"type":"object"}`), Resources: func(json.RawMessage) ([]string, error) { return nil, nil }, Execute: func(context.Context, json.RawMessage) (string, error) { return `{"status":"stale"}`, nil }})
	reg, err := automation.NewRegistry(nodes...)
	if err != nil {
		t.Fatal(err)
	}
	source := json.RawMessage(`{"version":3,"name":"Monitor","steps":[{"id":"check","tool":"echo","review":true,"args":{}}],"outputs":{"result":{"$step":"check"}}}`)
	trigger := store.Schedule{ID: "minute", RecipePath: "monitor.json", Source: source, Cron: "* * * * *", Timezone: "UTC", Missed: "once"}
	if err := db.PutSchedule(t.Context(), trigger); err != nil {
		t.Fatal(err)
	}
	schedules, _ := db.Schedules(t.Context())
	due := schedules[0].Next
	// The worker wakes late: queued time and intended time must differ.
	if err := db.Tick(t.Context(), due.Add(8*time.Minute)); err != nil {
		t.Fatal(err)
	}
	job, ok, err := db.Claim(t.Context())
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if job.ScheduledFor == nil || !job.ScheduledFor.Equal(due) || job.Created.Equal(due) {
		t.Fatal(job)
	}
	allow := func(context.Context, string, string, json.RawMessage) error { return nil }
	svc := Service{WS: ws, StoreDir: base, Registry: reg, AuthorizeResource: allow}
	if err := svc.ExecuteJob(t.Context(), db, job); err != nil {
		t.Fatal(err)
	}
	jobs, _ := db.Jobs(t.Context())
	job = jobs[0]
	if job.RunID == "" {
		t.Fatal(job.Error)
	}
	r, err := db.Run(job.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "waiting_review" || r.Record.ScheduleID != "minute" || r.Record.ScheduledFor == nil || !r.Record.ScheduledFor.Equal(due) {
		t.Fatal(r)
	}
	if _, err := svc.Review(t.Context(), job.RunID, "check", true); err != nil {
		t.Fatal(err)
	}
	r, err = db.Run(job.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "completed" || r.Record.ScheduleID != "minute" || r.Record.ScheduledFor == nil || !r.Record.ScheduledFor.Equal(due) {
		t.Fatal(r)
	}
	summary := InspectRun(r)
	if string(summary.Outputs.Value) != `{"result":{"status":"stale"}}` || summary.Status != "completed" {
		t.Fatal(summary)
	}
	queued, err := db.Enqueue(t.Context(), "monitor.json", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if queued.ScheduledFor != nil {
		t.Fatal("manual submission invented a scheduled time")
	}
	if err := db.UpdateJob(t.Context(), queued.ID, func(j *store.Job) error { j.Schedule = "minute"; j.Created = job.Created.Add(time.Second); return nil }); err != nil {
		t.Fatal(err)
	}
	jobs, _ = db.Jobs(t.Context())
	views, err := svc.InspectSchedules(t.Context(), db, schedules, jobs)
	if err != nil || len(views) != 1 {
		t.Fatal(views, err)
	}
	v := views[0]
	if v.LatestJob == nil || v.LatestJob.ID != queued.ID || v.LatestRun == nil || v.LatestRun.ID != job.RunID || v.LatestRun.Status != "completed" {
		t.Fatal(v)
	}
}
