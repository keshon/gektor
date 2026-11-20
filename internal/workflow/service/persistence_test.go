package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
)

func rejectCompletion(t *testing.T, db *store.Store, id string) *sql.DB {
	t.Helper()
	writer, _, err := db.EditRun(id)
	if err != nil {
		t.Fatal(err)
	}
	path := writer.DatabasePath()
	err = writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	sqlite, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlite.Close() })
	_, err = sqlite.Exec(`CREATE TRIGGER reject_completion BEFORE UPDATE ON runs WHEN NEW.status='completed'
BEGIN SELECT RAISE(ABORT, 'completion checkpoint fault'); END`)
	if err != nil {
		t.Fatal(err)
	}
	return sqlite
}

func TestReviewedAndRecoveredJobsRetainCheckpointFailureIdentity(t *testing.T) {
	calls := 0
	svc, db, job, _ := pendingJob(t, func(context.Context, json.RawMessage) (string, error) {
		calls++
		return `{}`, nil
	})
	sqlite := rejectCompletion(t, db, job.RunID)
	_, err := svc.Review(t.Context(), job.RunID, "review", true)
	var persistence *persistenceError
	if !errors.As(err, &persistence) || !errors.Is(err, persistence.error) || calls != 1 {
		t.Fatal("review lost persistence failure or cause", calls, err)
	}
	current, err := db.Job(t.Context(), job.ID)
	if err != nil || current.Status != "interrupted" {
		t.Fatal("review reported an uncommitted completion", current, err)
	}
	request := workflow.Recovery{Note: "All effects are committed; only run completion is missing"}
	_, err = svc.Recover(t.Context(), job.RunID, request)
	if !errors.As(err, &persistence) || !errors.Is(err, persistence.error) || calls != 1 {
		t.Fatal("recovery lost persistence failure or replayed an effect", calls, err)
	}
	_, err = sqlite.Exec("DROP TRIGGER reject_completion")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Recover(t.Context(), job.RunID, request)
	if err != nil || calls != 1 {
		t.Fatal("recovery repeated a completed effect", calls, err)
	}
	current, err = db.Job(t.Context(), job.ID)
	if err != nil || current.Status != "completed" {
		t.Fatal("recovery did not finalize the job", current, err)
	}
}

func TestUnlinkedWaitContinuationReturnsCheckpointFailures(t *testing.T) {
	directory, base := t.TempDir(), t.TempDir()
	svc := recoveryService(t, directory, base)
	source := []byte(`{"version":4,"name":"Wait","steps":[{"id":"delay","tool":"workflow_wait","args":{"delay_ms":1}},{"id":"effect","tool":"probe","depends_on":["delay"],"args":{"label":"after"}}]}`)
	out, err := svc.RunSource(t.Context(), source, "wait.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := out
	db, err := store.OpenStore(base, directory)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rejectCompletion(t, db, summary.RunID)
	writer, run, claimed, err := db.ClaimWait(t.Context(), time.Now())
	if err != nil || !claimed {
		t.Fatal("wait was not claimable", claimed, err)
	}
	defer writer.Close()
	err = svc.ContinueWait(t.Context(), db, writer, run)
	var persistence *persistenceError
	if !errors.As(err, &persistence) || !errors.Is(err, persistence.error) {
		t.Fatal("unlinked wait swallowed a persistence failure", err)
	}
	err = writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	state, err := db.Run(summary.RunID)
	if err != nil || state.Status != "interrupted" || state.Record.Status != "running" {
		t.Fatal("wait lost its recovery state", state, err)
	}
}
