package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
	callnode "github.com/keshon/gektor/internal/workflow/nodes/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
)

func recoveryService(t *testing.T, directory, base string) Service {
	t.Helper()
	ws, err := workspace.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	nodes := callnode.Definitions()
	nodes = append(nodes, workflow.Node{
		Name: "probe", Version: 1, Concurrent: true,
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"],"additionalProperties":false}`),
		Resources:    func(json.RawMessage) ([]string, error) { return nil, nil },
		Execute: func(_ context.Context, raw json.RawMessage) (string, error) {
			var in struct{ Label string }
			err := json.Unmarshal(raw, &in)
			if err != nil {
				return "", err
			}
			file, err := os.OpenFile(filepath.Join(directory, "effects.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				return "", err
			}
			_, writeErr := fmt.Fprintln(file, in.Label)
			err = errors.Join(writeErr, file.Close())
			if err != nil {
				return "", err
			}
			if in.Label == "uncertain" && os.Getenv("GEKTOR_RECOVERY_HELPER") == "1" {
				// Simulate a process disappearing after an effect, before its result commits.
				os.Exit(0)
			}
			return `{"label":"after"}`, nil
		},
	})
	registry, err := workflow.NewRegistry(nodes...)
	if err != nil {
		t.Fatal(err)
	}
	return Service{
		WS: ws, StoreDir: base, Registry: registry,
		Authorize:         func(context.Context, string, json.RawMessage) error { return nil },
		AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil },
	}
}

func TestRecoveryProcessHelper(t *testing.T) {
	if os.Getenv("GEKTOR_RECOVERY_HELPER") != "1" {
		return
	}
	svc := recoveryService(t, os.Getenv("GEKTOR_RECOVERY_WORKSPACE"), os.Getenv("GEKTOR_RECOVERY_STORE"))
	svc.OnRun = func(id string) error { _, err := fmt.Fprintln(os.Stdout, id); return err }
	_, err := svc.Run(t.Context(), "root.json", nil)
	t.Fatal("helper did not stop during the effect", err)
}

func interruptedRecovery(t *testing.T, mode string) (Service, *store.Store, string) {
	t.Helper()
	directory, base := t.TempDir(), t.TempDir()
	body := `[{"id":"before","tool":"probe","args":{"label":"before"}},{"id":"effect","tool":"probe","args":{"label":"uncertain"}},{"id":"after","tool":"probe","depends_on":["effect"],"args":{"label":{"$step":"effect","pointer":"/label"}}}]`
	source := `{"version":4,"name":"Recovery","steps":` + body + `,"outputs":{"result":{"$step":"after"},"done":true,"next":{}}}`
	if mode == "collection" || mode == "child collection" {
		source = `{"version":4,"name":"Collection","steps":[{"id":"batch","tool":"probe","args":{},"for_each":[{"label":"before"},{"label":"uncertain"},{"label":"after"}]}],"outputs":{"items":{"$step":"batch"}}}`
	}
	if mode == "child" || mode == "child collection" || mode == "map" || mode == "repeat" {
		err := os.WriteFile(filepath.Join(directory, "child.json"), []byte(source), 0600)
		if err != nil {
			t.Fatal(err)
		}
		policy := ""
		if mode == "map" {
			policy = `,"for_each":[{},{}]`
		}
		if mode == "repeat" {
			policy = `,"repeat":{"max_iterations":2,"until":"/done","next_inputs":"/next"}`
		}
		source = `{"version":4,"name":"Caller","steps":[{"id":"call","tool":"workflow_call","args":{"recipe_path":"child.json"}` + policy + `}]}`
	}
	err := os.WriteFile(filepath.Join(directory, "root.json"), []byte(source), 0600)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestRecoveryProcessHelper$")
	command.Env = append(os.Environ(), "GEKTOR_RECOVERY_HELPER=1", "GEKTOR_RECOVERY_WORKSPACE="+directory, "GEKTOR_RECOVERY_STORE="+base)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatal(string(out), err)
	}
	id := strings.TrimSpace(string(out))
	svc := recoveryService(t, directory, base)
	db, err := store.OpenStore(base, directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return svc, db, id
}

func TestOperatorRecoveryPreservesCompletedEffectsAcrossScopes(t *testing.T) {
	for _, mode := range []string{"root", "child", "collection", "child collection", "map", "repeat"} {
		for _, action := range []string{"complete", "retry"} {
			t.Run(mode+"/"+action, func(t *testing.T) {
				svc, db, id := interruptedRecovery(t, mode)
				state, err := db.Run(id)
				if err != nil || state.Status != "interrupted" {
					t.Fatal(state, err)
				}
				uncertain := workflow.UncertainSteps(state.Record)
				if len(uncertain) != 1 {
					t.Fatal("uncertain invocations", uncertain)
				}
				decision := workflow.RecoveryDecision{StepID: uncertain[0].ID, Action: action}
				if action == "complete" {
					decision.Output = json.RawMessage(`{"label":"after"}`)
				}
				out, err := svc.Recover(t.Context(), id, workflow.Recovery{Note: "Checked the remote outcome", Decisions: []workflow.RecoveryDecision{decision}})
				if err != nil {
					t.Fatal(out, err)
				}
				state, err = db.Run(id)
				if err != nil || state.Status != "completed" || len(state.Record.Recoveries) != 1 {
					t.Fatal(state, err)
				}
				effects, err := os.ReadFile(filepath.Join(svc.WS.Root(), "effects.txt"))
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if mode == "map" {
					want = 2
				}
				wantUncertain := want
				if action == "retry" {
					wantUncertain++
				}
				if strings.Count(string(effects), "before\n") != want || strings.Count(string(effects), "after\n") != want || strings.Count(string(effects), "uncertain\n") != wantUncertain {
					t.Fatal("replayed or lost effects", string(effects))
				}
				for _, step := range workflow.ScopedSteps(state.Record) {
					if step.ID == decision.StepID && len(step.Result.Attempts) != (1+map[string]int{"retry": 1}[action]) {
						t.Fatal("attempt history lost", step.Result.Attempts)
					}
				}
			})
		}
	}
}

func TestRecoveryRejectsInvalidDecisionsAndCurrentPolicy(t *testing.T) {
	svc, db, id := interruptedRecovery(t, "root")
	state, err := db.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	idStep := workflow.UncertainSteps(state.Record)[0].ID
	for _, test := range []struct {
		name    string
		request workflow.Recovery
	}{
		{"missing", workflow.Recovery{Note: "checked"}},
		{"unknown", workflow.Recovery{Note: "checked", Decisions: []workflow.RecoveryDecision{{StepID: "missing", Action: "retry"}}}},
		{"invalid output", workflow.Recovery{Note: "checked", Decisions: []workflow.RecoveryDecision{{StepID: idStep, Action: "complete", Output: json.RawMessage(`{"label":42}`)}}}},
		{"retry output", workflow.Recovery{Note: "checked", Decisions: []workflow.RecoveryDecision{{StepID: idStep, Action: "retry", Output: json.RawMessage(`{}`)}}}},
		{"blank note", workflow.Recovery{Decisions: []workflow.RecoveryDecision{{StepID: idStep, Action: "retry"}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := svc.Recover(t.Context(), id, test.request)
			if err == nil {
				t.Fatal("invalid recovery accepted")
			}
			state, err := db.Run(id)
			if err != nil || state.Status != "interrupted" || len(state.Record.Recoveries) != 0 {
				t.Fatal("invalid recovery changed checkpoint", state, err)
			}
		})
	}
	request := workflow.Recovery{Note: "remote command finished", Decisions: []workflow.RecoveryDecision{{StepID: idStep, Action: "complete", Output: json.RawMessage(`{"label":"after"}`)}}}
	for _, denied := range []string{"workflow_run", "probe"} {
		t.Run(denied, func(t *testing.T) {
			blocked := svc
			blocked.AuthorizeResource = func(_ context.Context, name, _ string, _ json.RawMessage) error {
				if name == denied {
					return fmt.Errorf("permission revoked")
				}
				return nil
			}
			_, err := blocked.Recover(t.Context(), id, request)
			if err == nil {
				t.Fatal("recovery bypassed revoked permission")
			}
		})
	}
	w, _, err := db.EditRun(id)
	if err != nil {
		t.Fatal(err)
	}
	_, recoverErr := svc.Recover(t.Context(), id, request)
	closeErr := w.Close()
	if recoverErr == nil || closeErr != nil {
		t.Fatal("active run recovered", recoverErr, closeErr)
	}

	job, err := db.Enqueue(t.Context(), state.Record.RecipePath, json.RawMessage(state.Record.RecipeSource), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = db.UpdateJob(t.Context(), job.ID, func(j *store.Job) error { j.Status, j.RunID, j.Cancel = "interrupted", id, true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Recover(t.Context(), id, request)
	if err == nil {
		t.Fatal("cancelled job recovered")
	}
	err = db.UpdateJob(t.Context(), job.ID, func(j *store.Job) error { j.Cancel, j.Status = false, "failed"; return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Recover(t.Context(), id, request)
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := db.Jobs(t.Context())
	if err != nil || jobs[0].Status != "completed" || jobs[0].RunID != id {
		t.Fatal(jobs, err)
	}
	_, err = svc.Recover(t.Context(), id, request)
	if err == nil {
		t.Fatal("completed run recovered again")
	}
}

func TestRecoveryPreservesAcceptedCallbackBeforeConsumption(t *testing.T) {
	svc := recoveryService(t, t.TempDir(), t.TempDir())
	source := `{"version":4,"name":"Recover callback","steps":[{"id":"wait","tool":"workflow_wait_callback","args":{"channel":"approve","timeout_ms":60000}},{"id":"after","tool":"probe","args":{"label":"after"}}]}`
	out, err := svc.RunSource(t.Context(), []byte(source), "root.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		ID string `json:"run_id"`
	}
	err = json.Unmarshal([]byte(out), &summary)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.OpenStore(svc.StoreDir, svc.WS.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	state, err := db.Run(summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	callback := workflow.PendingCallback(state.Record)
	_, err = db.AcceptCallback(t.Context(), store.CallbackDelivery{RunID: summary.ID, WaitID: callback.ID, Channel: callback.Channel, EventID: "accepted", Payload: json.RawMessage(`{"ok":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	w, r, err := db.EditRun(summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.Status, r.WakeAt = "running", nil
	r.Steps[0].Status = "running"
	err = w.Save(r)
	closeErr := w.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	request := workflow.Recovery{Note: "Worker stopped before callback consumption"}
	blocked := svc
	blocked.AuthorizeResource = func(_ context.Context, name, _ string, _ json.RawMessage) error {
		if name == "workflow_wait_callback" {
			return fmt.Errorf("permission revoked")
		}
		return nil
	}
	_, err = blocked.Recover(t.Context(), summary.ID, request)
	if err == nil {
		t.Fatal("callback permission bypassed")
	}
	_, err = svc.Recover(t.Context(), summary.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	state, err = db.Run(summary.ID)
	if err != nil || state.Status != "completed" || !strings.Contains(state.Record.Steps[0].Output, `"event_id":"accepted"`) {
		t.Fatal(state, err)
	}
}
