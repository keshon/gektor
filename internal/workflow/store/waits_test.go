package store

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func TestWaitCheckpointAndDeadlineCommitTogether(t *testing.T) {
	s, _, _ := testStore(t)
	writer, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	r := workflow.Run{Status: "running", Steps: []workflow.StepResult{{ID: "delay", Status: "pending"}}}
	err = writer.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`CREATE TRIGGER reject_wakeup BEFORE INSERT ON wakeups BEGIN SELECT RAISE(ABORT, 'wake fault'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	r.Status, r.WakeAt = "waiting", &deadline
	r.Steps[0].Status, r.Steps[0].Wait = "waiting", &workflow.WaitState{ResumeAt: deadline}
	err = writer.Save(r, 0)
	if err == nil {
		t.Fatal("wake-up fault was ignored")
	}
	state, err := s.Run(writer.ID())
	if err != nil || state.Record.Status != "running" || state.Record.Steps[0].Status != "pending" || state.Record.WakeAt != nil {
		t.Fatal("partial wait checkpoint became visible", state, err)
	}
	_, err = s.db.Exec("DROP TRIGGER reject_wakeup")
	if err != nil {
		t.Fatal(err)
	}
	err = writer.Save(r, 0)
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.Run(writer.ID())
	if err != nil || state.Record.WakeAt == nil || !state.Record.WakeAt.Equal(deadline) {
		t.Fatal("deadline did not round trip", state, err)
	}
}

func TestWaitProcessHelper(t *testing.T) {
	mode := os.Getenv("GEKTOR_WAIT_CRASH")
	if mode == "" {
		return
	}
	db, err := OpenStore(os.Getenv("GEKTOR_WAIT_STORE"), os.Getenv("GEKTOR_WAIT_WORKSPACE"))
	if err != nil {
		t.Fatal(err)
	}
	writer, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
	r := workflow.Run{Status: "waiting", WakeAt: &deadline, Steps: []workflow.StepResult{{ID: "delay", Status: "waiting", Wait: &workflow.WaitState{ResumeAt: deadline}}}}
	err = writer.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "running" {
		r.Status, r.WakeAt = "running", nil
		err = writer.Save(r)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = fmt.Fprintln(os.Stdout, writer.ID())
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately bypass cleanup to exercise OS lease release and committed SQLite state.
	os.Exit(0)
}

func TestWaitSurvivesProcessExitWithoutReplayingRunningWork(t *testing.T) {
	for _, mode := range []string{"waiting", "running"} {
		t.Run(mode, func(t *testing.T) {
			base, workspace := t.TempDir(), t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestWaitProcessHelper$")
			cmd.Env = append(os.Environ(), "GEKTOR_WAIT_CRASH="+mode, "GEKTOR_WAIT_STORE="+base, "GEKTOR_WAIT_WORKSPACE="+workspace)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatal(string(out), err)
			}
			id := strings.TrimSpace(string(out))
			db, err := OpenStore(base, workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			writer, r, ok, err := db.ClaimWait(t.Context(), time.Now())
			if writer != nil {
				defer writer.Close()
			}
			if err != nil || ok != (mode == "waiting") {
				t.Fatal("incorrect recovery", mode, ok, err)
			}
			if ok && (r.RunID != id || r.Status != "waiting") {
				t.Fatal("wrong resumed run", r)
			}
			if mode == "running" {
				state, readErr := db.Run(id)
				if readErr != nil || state.Status != "interrupted" {
					t.Fatal("uncertain execution was made resumable", state, readErr)
				}
			}
		})
	}
}

func TestCancelledJobBecomingWaitingIsReconciled(t *testing.T) {
	db, _, _ := testStore(t)
	job, err := db.Enqueue(t.Context(), "recipe.json", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(24 * time.Hour)
	r := workflow.Run{Status: "waiting", WakeAt: &deadline, Steps: []workflow.StepResult{{ID: "delay", Status: "waiting", Wait: &workflow.WaitState{ResumeAt: deadline}}}}
	err = writer.Save(r)
	if err != nil {
		writer.Close()
		t.Fatal(err)
	}
	err = writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = db.UpdateJob(t.Context(), job.ID, func(j *Job) error {
		j.Status, j.RunID, j.Cancel = "running", writer.ID(), true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.ReconcileRuns(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	claimed, record, ok, err := db.ClaimWait(t.Context(), time.Now())
	if claimed != nil {
		defer claimed.Close()
	}
	if err != nil || !ok || record.RunID != writer.ID() {
		t.Fatal("racing cancellation left a long wait", ok, err)
	}
}
