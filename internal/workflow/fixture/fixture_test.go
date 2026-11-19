package fixture

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

const sample = `{"version":1,"inputs":{},"calls":[{"step_id":"fetch","operation":"http_request","node_version":1,"args":{"url":"https://example.com/"},"responses":[{"failure":{"code":"http_status","message":"temporary"}},{"output":{"status_code":200}}]}],"expect":{"status":"completed","outputs":{"ok":true}}}`

func TestFixtureRequiresExactCallsAndConsumesRetryHistory(t *testing.T) {
	session, err := New([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	step := workflow.Step{ID: "fetch", Tool: "http_request", Args: json.RawMessage(`{"url":"https://example.com/"}`)}
	_, err = session.Run(t.Context(), step)
	failure := workflow.OperationFailure(err)
	if failure == nil || failure.Code != "http_status" {
		t.Fatal(err)
	}
	output, err := session.Run(t.Context(), step)
	if err != nil || output != `{"status_code":200}` {
		t.Fatal(output, err)
	}
	err = session.Check("completed", json.RawMessage(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Run(t.Context(), step)
	if err == nil || workflow.OperationFailure(err) != nil {
		t.Fatal("exhaustion must be terminal", err)
	}
	for _, altered := range []workflow.Step{
		{ID: "missing", Tool: step.Tool, Args: step.Args},
		{ID: step.ID, Tool: "ssh_exec", Args: step.Args},
		{ID: step.ID, Tool: step.Tool, NodeVersion: 2, Args: step.Args},
		{ID: step.ID, Tool: step.Tool, Args: json.RawMessage(`{"url":"https://other.example/"}`)},
	} {
		fresh, createErr := New([]byte(sample))
		if createErr != nil {
			t.Fatal(createErr)
		}
		_, err = fresh.Run(t.Context(), altered)
		if err == nil || workflow.OperationFailure(err) != nil {
			t.Fatal("mismatch must be terminal", altered, err)
		}
	}
}

func TestFixtureRejectsMalformedOrUnusedResponses(t *testing.T) {
	for _, raw := range []string{
		`null`, `{}`, sample + `{}`, strings.Replace(sample, `"version":1`, `"version":2`, 1),
		strings.Replace(sample, `"inputs":{}`, `"inputs":{},"unknown":true`, 1),
		strings.Replace(sample, `"operation":"http_request"`, `"operation":"script_transform"`, 1),
		strings.Replace(sample, `"operation":"http_request"`, `"operation":"workflow_join"`, 1),
		strings.Replace(sample, `"output":{"status_code":200}`, `"output":{},"failure":{"code":"x","message":"x"}`, 1),
	} {
		_, err := New([]byte(raw))
		if err == nil {
			t.Fatal("accepted invalid fixture", raw)
		}
	}
	session, err := New([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	err = session.Check("completed", json.RawMessage(`{"ok":true}`))
	if err == nil {
		t.Fatal("unused responses accepted")
	}
	matches, err := sameJSON(json.RawMessage(`{"n":9007199254740993,"x":true}`), json.RawMessage(`{"x":true,"n":9007199254740993}`))
	if err != nil || !matches {
		t.Fatal(matches, err)
	}
	matches, err = sameJSON(json.RawMessage(`9007199254740993`), json.RawMessage(`9007199254740992`))
	if err != nil || matches {
		t.Fatal("large numbers lost precision", matches, err)
	}
}

func TestFixtureConcurrentConsumptionAndCancellation(t *testing.T) {
	session, err := New([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	step := workflow.Step{ID: "fetch", Tool: "http_request", Args: json.RawMessage(`{"url":"https://example.com/"}`)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = session.Run(ctx, step)
	if err != context.Canceled {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	results := make(chan error, 2)
	workers.Add(2)
	for range 2 {
		go func() {
			defer workers.Done()
			_, callErr := session.Run(t.Context(), step)
			results <- callErr
		}()
	}
	workers.Wait()
	close(results)
	failures := 0
	for callErr := range results {
		if callErr != nil {
			if workflow.OperationFailure(callErr) == nil {
				t.Fatal(callErr)
			}
			failures++
		}
	}
	if failures != 1 {
		t.Fatal("concurrent responses were lost", failures)
	}
	err = session.Check("completed", json.RawMessage(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCaptureKeepsInputsAndTypedFailures(t *testing.T) {
	now := time.Now().UTC()
	run := workflow.Run{Status: "completed", Inputs: map[string]json.RawMessage{"count": json.RawMessage(`9007199254740993`)}, Definition: workflow.Definition{Steps: []workflow.Step{{ID: "fetch", Tool: "http_request"}}}, Steps: []workflow.StepResult{{ID: "fetch", Status: "completed", Args: json.RawMessage(`{"url":"https://example.com/"}`), Output: `{"ok":true}`, Attempts: []workflow.Attempt{{Number: 1, Finished: &now, Failure: &workflow.Failure{Code: "http_status", Message: "temporary"}}, {Number: 2, Finished: &now}}}}}
	raw, err := Capture(run, json.RawMessage(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	session, err := New(raw)
	if err != nil || string(session.Inputs["count"]) != "9007199254740993" || len(session.spec.Calls[0].Responses) != 2 {
		t.Fatal(string(raw), err)
	}
	for _, status := range []string{"running", "waiting_review", "waiting", "failed"} {
		run.Status = status
		_, err = Capture(run, nil)
		if err == nil {
			t.Fatal("captured unfinished run", status)
		}
	}
	run.Status = "completed"
	run.Definition.Steps[0].Tool = "script_transform"
	run.Definition.Steps[0].Review = true
	_, err = Capture(run, nil)
	if err == nil {
		t.Fatal("captured review as a pure operation")
	}
}
