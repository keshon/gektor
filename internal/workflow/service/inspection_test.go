package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/store"
)

func TestInvocationPagesAreBoundedAndKeepScopedIDs(t *testing.T) {
	run := workflow.Run{RunID: "inspection", Status: "running", Definition: workflow.Definition{Steps: []workflow.Step{{ID: "batch", Tool: "probe"}}}}
	batch := workflow.StepResult{ID: "batch", Status: "running"}
	for index := range 150 {
		batch.Iterations = append(batch.Iterations, workflow.StepResult{ID: fmt.Sprintf("batch[%d]", index), Status: "completed", Args: json.RawMessage(`{"private":"argument-marker"}`), Output: `{"private":"output-marker"}`})
	}
	run.Steps = []workflow.StepResult{batch}
	state := store.RunState{Record: run, Status: "interrupted"}
	for _, tc := range []struct{ offset, limit, count, next int }{
		{0, 0, 50, 50}, {50, 100, 100, 150}, {150, 100, 1, 0}, {151, 10, 0, 0}, {int(^uint(0) >> 1), 10, 0, 0},
	} {
		value, err := InspectRecord(state, Inspection{View: "invocations", Offset: tc.offset, Limit: tc.limit})
		if err != nil {
			t.Fatal(err)
		}
		page := value.(InvocationPage)
		if len(page.Invocations) != tc.count || page.Total != 151 || page.NextOffset != tc.next || page.Status != "interrupted" {
			t.Fatal(page)
		}
		encoded, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "argument-marker") || strings.Contains(string(encoded), "output-marker") {
			t.Fatal("invocation metadata leaked payloads")
		}
		if tc.offset == 150 && page.Invocations[0].ID != "batch[149]" {
			t.Fatal(page)
		}
	}
}

func TestAttemptInspectionPreservesHistoryWithoutChangingRun(t *testing.T) {
	started := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	finished := started.Add(time.Second)
	attempts := []workflow.Attempt{
		{Number: 1, Started: started, Finished: &finished, Error: "temporary", Failure: &workflow.Failure{Code: "http_status", Message: "temporary"}},
		{Number: 2, Started: finished},
	}
	child := workflow.Run{Definition: workflow.Definition{Steps: []workflow.Step{{ID: "fetch", Tool: "http_request"}}}, Steps: []workflow.StepResult{{ID: "fetch", Status: "running", Attempts: attempts}}}
	state := store.RunState{Status: "interrupted", Record: workflow.Run{RunID: "saved", Definition: workflow.Definition{Steps: []workflow.Step{{ID: "call", Tool: "workflow_call"}}}, Steps: []workflow.StepResult{{ID: "call", Child: &child}}}}
	before, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	value, err := InspectRecord(state, Inspection{View: "attempts", StepID: "call/fetch"})
	if err != nil {
		t.Fatal(err)
	}
	history := value.(AttemptInspection)
	if history.Step.ID != "call/fetch" || history.Step.Attempts != 2 || len(history.Attempts) != 2 || history.Attempts[0].Failure.Code != "http_status" || history.Attempts[1].Finished != nil {
		t.Fatal(history)
	}
	after, err := json.Marshal(state)
	if err != nil || string(before) != string(after) {
		t.Fatal("inspection mutated run", err)
	}
	for _, selection := range []Inspection{
		{View: "attempts"}, {View: "attempts", StepID: "missing"}, {View: "attempts", StepID: "call/fetch", Pointer: new(string)},
		{View: "invocations", Offset: -1}, {View: "invocations", Limit: 101}, {View: "step", StepID: "call/fetch", Offset: 1},
		{View: "summary", StepID: "call/fetch"}, {View: "invalid"},
	} {
		_, err = InspectRecord(state, selection)
		if err == nil {
			t.Fatal("accepted invalid inspection", selection)
		}
	}
}
