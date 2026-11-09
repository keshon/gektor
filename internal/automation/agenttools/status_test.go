package agenttools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestStatusReportsOwnershipAndLinkedRunsWithoutExecuting(t *testing.T) {
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
	trigger := store.Schedule{ID: "minute", RecipePath: "recipe.json", Source: json.RawMessage(`{"secret":"not in status"}`), Cron: "* * * * *", Timezone: "UTC"}
	if err := db.PutSchedule(t.Context(), trigger); err != nil {
		t.Fatal(err)
	}
	all, _ := db.Schedules(t.Context())
	if err := db.Tick(t.Context(), all[0].Next); err != nil {
		t.Fatal(err)
	}
	jobs, _ := db.Jobs(t.Context())
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	writer, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.Save(automation.Run{Status: "completed", RecipePath: "recipe.json", Started: time.Now(), Definition: automation.Definition{Name: "Test"}, Steps: []automation.StepResult{{ID: "@outputs", Status: "completed", Output: `{"status":"stale"}`}}}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateJob(t.Context(), jobs[0].ID, func(j *store.Job) error { j.Status = "completed"; j.RunID = writer.ID(); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("recipe.json", trigger.Source, 0600); err != nil {
		t.Fatal(err)
	}
	tool := Status{WS: ws, StoreDir: base, CheckSource: allow}
	check := func(held bool, schedule string, count int) {
		t.Helper()
		out, err := tool.Run(t.Context(), json.RawMessage(`{"schedule_id":"`+schedule+`","limit":1}`))
		if err != nil {
			t.Fatal(err)
		}
		var status struct {
			Held bool `json:"worker_lease_held"`
			Jobs []struct {
				RunID string `json:"run_id"`
			}
			Runs []struct {
				ID string `json:"run_id"`
			}
		}
		if err := json.Unmarshal([]byte(out), &status); err != nil {
			t.Fatal(err)
		}
		if status.Held != held || len(status.Jobs) != count || len(status.Runs) != count || strings.Contains(out, "not in status") {
			t.Fatal(out)
		}
		if count > 0 && (status.Jobs[0].RunID != writer.ID() || status.Runs[0].ID != writer.ID()) {
			t.Fatal(out)
		}
	}
	out, err := tool.Run(t.Context(), json.RawMessage(`{"schedule_id":"minute"}`))
	if err != nil || !strings.Contains(out, `"snapshot":{"state":"unchanged"}`) || !strings.Contains(out, `"outputs":{"value":{"status":"stale"}`) || !strings.Contains(out, `"scheduled_for"`) {
		t.Fatal(out, err)
	}
	check(false, "minute", 1)
	lease, err := db.DaemonLease()
	if err != nil {
		t.Fatal(err)
	}
	check(true, "minute", 1)
	check(true, "other", 0)
	lease.Close()
	check(false, "minute", 1)
	jobs, _ = db.Jobs(t.Context())
	if len(jobs) != 1 || jobs[0].Status != "completed" {
		t.Fatal("status mutated queue", jobs)
	}
}
