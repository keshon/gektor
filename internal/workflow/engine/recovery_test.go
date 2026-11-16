package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func recoveryCheckpoint(t *testing.T) workflow.Run {
	t.Helper()
	d := workflow.Definition{Version: 4, Name: "Recover collection", Steps: []workflow.Step{{ID: "batch", Tool: "probe", Args: json.RawMessage(`{}`), ForEach: json.RawMessage(`[{},{},{}]`), Concurrency: 2}}}
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) { return `{}`, nil }}
	r, err := Run(t.Context(), d, h, func(workflow.Run, ...int) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	r.Status, r.Finished = "running", nil
	r.Steps[0].Status = "running"
	for i := 1; i < 3; i++ {
		r.Steps[0].Iterations[i].Status = "running"
		r.Steps[0].Iterations[i].Output = ""
	}
	return r
}

func TestRecoveryDecisionsCommitBeforeSelectedCollectionRetries(t *testing.T) {
	r := recoveryCheckpoint(t)
	request := workflow.Recovery{Note: "checked both targets", Decisions: []workflow.RecoveryDecision{{StepID: "batch[1]", Action: "complete", Output: json.RawMessage(`{"verified":true}`)}, {StepID: "batch[2]", Action: "retry"}}}
	calls := 0
	committed := false
	h := collectionHandler{execute: func(_ context.Context, step workflow.Step) (string, error) {
		if !committed || step.ID != "batch[2]" {
			t.Error("unexpected replay before recovery commit", step.ID)
		}
		calls++
		return `{}`, nil
	}}
	save := func(r workflow.Run, _ ...int) error {
		if !committed && (len(r.Recoveries) != 1 || r.Steps[0].Iterations[1].Status != "completed" || r.Steps[0].Iterations[2].Status != "pending") {
			t.Fatal("incomplete recovery checkpoint", r)
		}
		committed = true
		return nil
	}
	recovered, err := Recover(t.Context(), r, request, h, save)
	if err != nil || calls != 1 || recovered.Status != "completed" || len(recovered.Steps[0].Iterations[2].Attempts) != 2 {
		t.Fatal(recovered, err, calls)
	}
	if r.Steps[0].Iterations[1].Status != "running" {
		t.Fatal("caller checkpoint mutated")
	}

	calls, committed = 0, false
	_, err = Recover(t.Context(), r, request, h, func(workflow.Run, ...int) error { return errors.New("disk full") })
	if err == nil || calls != 0 {
		t.Fatal("effects after failed recovery commit", calls, err)
	}
	var checkpoint workflow.Run
	_, err = Recover(t.Context(), r, request, h, func(record workflow.Run, _ ...int) error {
		if committed {
			return errors.New("process stopped after decisions committed")
		}
		raw, encodeErr := json.Marshal(record)
		if encodeErr != nil {
			return encodeErr
		}
		encodeErr = json.Unmarshal(raw, &checkpoint)
		committed = true
		return encodeErr
	})
	if err == nil || calls != 0 {
		t.Fatal("effect after checkpoint stop", calls, err)
	}
	if len(workflow.UncertainSteps(checkpoint)) != 0 {
		t.Fatal("committed decisions became uncertain again")
	}
	recovered, err = Recover(t.Context(), checkpoint, workflow.Recovery{Note: "Stopped before retry dispatch"}, h, func(workflow.Run, ...int) error { return nil })
	if err != nil || calls != 1 || recovered.Status != "completed" || len(recovered.Recoveries) != 2 {
		t.Fatal(recovered, calls, err)
	}
}

func TestRecoveryRequiresEveryUncertainItem(t *testing.T) {
	r := recoveryCheckpoint(t)
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
		t.Fatal("effect during rejected recovery")
		return "", nil
	}}
	for _, decisions := range [][]workflow.RecoveryDecision{
		{{StepID: "batch[1]", Action: "retry"}},
		{{StepID: "batch[1]", Action: "retry"}, {StepID: "batch[1]", Action: "retry"}},
		{{StepID: "batch[0]", Action: "retry"}, {StepID: "batch[2]", Action: "retry"}},
	} {
		_, err := Recover(t.Context(), r, workflow.Recovery{Note: "checked", Decisions: decisions}, h, func(workflow.Run, ...int) error { t.Fatal("saved invalid recovery"); return nil })
		if err == nil {
			t.Fatal("incomplete recovery accepted")
		}
	}
}

func TestRecoveryFailureKeepsLaterCompletedCollectionItems(t *testing.T) {
	r := recoveryCheckpoint(t)
	request := workflow.Recovery{Note: "verified later item; retry earlier target", Decisions: []workflow.RecoveryDecision{
		{StepID: "batch[1]", Action: "retry"},
		{StepID: "batch[2]", Action: "complete", Output: json.RawMessage(`{"verified":true}`)},
	}}
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) { return "", errors.New("permission denied") }}
	recovered, err := Recover(t.Context(), r, request, h, func(workflow.Run, ...int) error { return nil })
	if err == nil || recovered.Steps[0].Iterations[2].Status != "completed" || recovered.Steps[0].Iterations[2].Output != `{"verified":true}` {
		t.Fatal("completed result lost during failure", recovered, err)
	}
}
