package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func TestWaitCannotResumeEarlyOrResetItsDeadline(t *testing.T) {
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	waitCalls, effects, saves := 0, 0, 0
	h := collectionHandler{execute: func(_ context.Context, s workflow.Step) (string, error) {
		if s.Tool == "workflow_wait" {
			waitCalls++
			out, err := json.Marshal(workflow.WaitState{ResumeAt: deadline})
			return string(out), err
		}
		effects++
		return `{}`, nil
	}}

	d := workflow.Definition{Version: 4, Name: "Wait", Steps: []workflow.Step{
		{ID: "delay", Tool: "workflow_wait", Args: json.RawMessage(`{}`)},
		{ID: "effect", Tool: "probe", Args: json.RawMessage(`{}`)},
	}}

	save := func(workflow.Run, ...int) error { saves++; return nil }
	r, err := Run(t.Context(), d, h, save)
	if err != nil || r.Status != "waiting" || effects != 0 || waitCalls != 1 {
		t.Fatal(r, err)
	}

	before := saves
	_, err = Resume(t.Context(), r, h, save)
	if err == nil || saves != before || effects != 0 || waitCalls != 1 {
		t.Fatal("early resume changed execution", err)
	}

	past := time.Now().UTC().Add(-time.Second)
	r.WakeAt, r.Steps[0].Wait = &past, &workflow.WaitState{ResumeAt: past}
	r, err = Resume(t.Context(), r, h, save)
	if err != nil || r.Status != "completed" || effects != 1 || waitCalls != 1 || len(r.Steps[0].Attempts) != 1 {
		t.Fatal("deadline was recalculated or effect replayed", r, err)
	}

	_, err = Resume(t.Context(), r, h, save)
	if err == nil || effects != 1 {
		t.Fatal("terminal continuation replayed work", err)
	}
}

func TestWaitCheckpointFailureDoesNotReportAWaitingRun(t *testing.T) {
	d := workflow.Definition{Version: 4, Name: "Wait", Steps: []workflow.Step{{ID: "delay", Tool: "workflow_wait", Args: json.RawMessage(`{}`)}}}
	h := collectionHandler{execute: func(context.Context, workflow.Step) (string, error) {
		deadline := time.Now().UTC().Add(time.Hour)
		raw, err := json.Marshal(workflow.WaitState{ResumeAt: deadline})
		return string(raw), err
	}}

	persisted := ""
	r, err := Run(t.Context(), d, h, func(record workflow.Run, _ ...int) error {
		if record.Status == "waiting" {
			return errors.New("wait checkpoint unavailable")
		}
		persisted = record.Status
		return nil
	})
	if err == nil || r.Status != "failed" || persisted != "failed" || r.WakeAt != nil {
		t.Fatal("uncommitted wait was reported as resumable", r, err)
	}
}
