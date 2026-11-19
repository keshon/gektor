package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func TestPruneRunsPreservesLiveAndRecoverableHistory(t *testing.T) {
	s, _, _ := testStore(t)
	before := time.Now().Add(-24 * time.Hour)
	old := before.Add(-time.Hour)
	ids := map[string]string{}
	for _, kind := range []string{"completed", "failed", "cancelled", "waiting_review", "waiting", "running", "interrupted", "held", "unfinished", "young", "boundary", "linked_active", "linked_interrupted"} {
		writer, err := s.NewRun()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { writer.Close() })
		_, err = writer.Artifacts("save").Put(t.Context(), "evidence", strings.NewReader("keep unless pruned"))
		if err != nil {
			t.Fatal(err)
		}
		r := workflow.Run{Status: kind, Finished: &old}
		switch kind {
		case "held", "unfinished", "young", "boundary", "linked_active", "linked_interrupted":
			r.Status = "completed"
		}
		if kind == "unfinished" {
			r.Finished = nil
		}
		if kind == "young" {
			young := before.Add(time.Hour)
			r.Finished = &young
		}
		if kind == "boundary" {
			r.Finished = &before
		}
		if kind == "waiting" {
			r.WakeAt = &old
		}
		err = writer.Save(r)
		if err != nil {
			t.Fatal(err)
		}
		ids[kind] = writer.ID()
		if kind == "linked_active" || kind == "linked_interrupted" {
			job, enqueueErr := s.Enqueue(t.Context(), "recipe.json", json.RawMessage(`{}`), nil)
			if enqueueErr != nil {
				t.Fatal(enqueueErr)
			}
			err = s.UpdateJob(t.Context(), job.ID, func(j *Job) error {
				j.Status, j.RunID = "running", writer.ID()
				if kind == "linked_interrupted" {
					j.Status = "interrupted"
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		if kind != "held" {
			err = writer.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	removed, err := s.PruneRuns(t.Context(), before)
	if err != nil || len(removed) != 3 {
		t.Fatal(removed, err)
	}
	for kind, id := range ids {
		wantPruned := kind == "completed" || kind == "failed" || kind == "cancelled"
		if slices.Contains(removed, id) != wantPruned {
			t.Fatal("incorrect retention decision", kind, removed)
		}
		_, err = s.Run(id)
		if wantPruned && err == nil || !wantPruned && err != nil {
			t.Fatal("incorrect run retention", kind, err)
		}
		_, err = s.root.Stat(filepath.Join("runs", id, "artifacts"))
		if os.IsNotExist(err) != wantPruned {
			t.Fatal("incorrect artifact retention", kind, err)
		}
		_, err = s.root.Stat(filepath.Join("runs", id, "lock"))
		if err != nil {
			t.Fatal("persistent lease file removed", kind, err)
		}
	}
	removed, err = s.PruneRuns(t.Context(), before)
	if err != nil || len(removed) != 0 {
		t.Fatal("retention was not idempotent", removed, err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.PruneRuns(cancelled, before)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled retention was accepted", err)
	}
}

func TestPruneRunDeletionRollsBackJobHistoryAndCascadesStoredDetails(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := t.Context()
	writer, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	expires := time.Now().Add(time.Hour)
	r := workflow.Run{
		Status: "waiting", WakeAt: &expires,
		Steps: []workflow.StepResult{{
			ID: "callback", Status: "waiting",
			Callback: &workflow.CallbackState{ID: "receipt", StepID: "callback", Channel: "approve", ExpiresAt: expires},
			Attempts: []workflow.Attempt{{Number: 1}},
			Child:    &workflow.Run{Status: "completed", Finished: &old},
		}},
	}
	err = writer.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Status, r.Finished = "completed", &old
	err = writer.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	_, err = writer.Artifacts("save").Put(ctx, "evidence", strings.NewReader("data"))
	if err != nil {
		t.Fatal(err)
	}
	err = writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.EnqueueOnce(ctx, "recipe.json", json.RawMessage(`{}`), nil, "delivery-key", "request-hash")
	if err != nil {
		t.Fatal(err)
	}
	err = s.UpdateJob(ctx, job.ID, func(j *Job) error {
		j.Status, j.RunID = "completed", writer.ID()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec("CREATE TRIGGER reject_run_delete BEFORE DELETE ON runs BEGIN SELECT RAISE(ABORT, 'retention fault'); END")
	if err != nil {
		t.Fatal(err)
	}
	removed, err := s.PruneRuns(ctx, time.Now())
	if err == nil || len(removed) != 0 {
		t.Fatal("retention fault was hidden", removed, err)
	}
	_, err = s.Job(ctx, job.ID)
	if err != nil {
		t.Fatal("run deletion rolled back without its job", err)
	}
	_, err = s.Run(writer.ID())
	if err != nil {
		t.Fatal("failed retention lost run history", err)
	}
	_, err = s.db.Exec("DROP TRIGGER reject_run_delete")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"runs", "jobs"} {
		_, err = s.db.Exec("CREATE TRIGGER ignore_history_delete BEFORE DELETE ON " + table + " BEGIN SELECT RAISE(IGNORE); END")
		if err != nil {
			t.Fatal(err)
		}
		removed, err = s.PruneRuns(ctx, time.Now())
		if err == nil || len(removed) != 0 {
			t.Fatal("silently ignored history deletion was accepted", table, removed, err)
		}
		_, err = s.Job(ctx, job.ID)
		if err != nil {
			t.Fatal("ignored deletion lost linked job history", table, err)
		}
		_, err = s.db.Exec("DROP TRIGGER ignore_history_delete")
		if err != nil {
			t.Fatal(err)
		}
	}
	removed, err = s.PruneRuns(ctx, time.Now())
	if err != nil || len(removed) != 1 || removed[0] != writer.ID() {
		t.Fatal("retention could not finish after artifact cleanup", removed, err)
	}
	for _, table := range []string{"runs", "jobs", "invocations", "attempts", "scopes", "callbacks", "wakeups", "artifacts"} {
		var count int
		err = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count)
		if err != nil || count != 0 {
			t.Fatal("retained details after pruning", table, count, err)
		}
	}
	fresh, err := s.EnqueueOnce(ctx, "recipe.json", json.RawMessage(`{}`), nil, "delivery-key", "request-hash")
	if err != nil || fresh.ID == job.ID {
		t.Fatal("pruned job still deduplicated a delivery", fresh, err)
	}
}
