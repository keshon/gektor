package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

type unavailableResults struct{}

func TestCheckExitCodeOmitsUnavailableStatus(t *testing.T) {
	for _, err := range []error{&exec.ExitError{}, context.Canceled, errors.New("could not start")} {
		if code := checkExitCode(err); code != nil {
			t.Fatalf("unavailable process status became exit code %d", *code)
		}
	}
	if code := checkExitCode(nil); code == nil || *code != 0 {
		t.Fatal("successful command lost exit zero")
	}
}

func (unavailableResults) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestResultWriteFailureIsReported(t *testing.T) {
	err := writeProbeResult(unavailableResults{}, Result{Passed: true})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("result write error = %v", err)
	}
}

type timedClient struct{}

func (timedClient) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	time.Sleep(2 * time.Millisecond)
	req.OnProgress(llm.Progress{CompletionTokens: 1})
	return llm.ChatResponse{FinishReason: "length", Usage: llm.Usage{PromptTokens: 20, CompletionTokens: 2}}, context.DeadlineExceeded
}

func TestTimingRetainsFailedRequestAndForwardsProgress(t *testing.T) {
	c := &countingClient{inner: timedClient{}}
	n := 0
	_, err := c.Chat(t.Context(), llm.ChatRequest{OnProgress: func(llm.Progress) { n++ }})
	if !errors.Is(err, context.DeadlineExceeded) || n != 1 || len(c.timings) != 1 {
		t.Fatalf("%v %+v", err, c.timings)
	}
	r := c.timings[0]
	if r.Seconds <= 0 || r.FirstTokenSeconds <= 0 || r.FinishReason != "length" || r.Error == "" || r.CompletionTokens != 2 {
		t.Fatal(r)
	}
}

func TestArtifactChecksHaveIndependentBudgetAndRespectParentCancellation(t *testing.T) {
	ws := checkWorkspaceForTest(t)
	parent := t.Context()
	runCtx, cancel := context.WithTimeout(parent, time.Nanosecond)
	defer cancel()
	<-runCtx.Done()
	p := Probe{Verify: []Check{{Type: "shell", Cmd: "echo artifact-check"}}}
	checks := checkArtifacts(parent, p, ws)
	if len(checks) != 1 || !checks[0].Passed {
		t.Fatal(checks)
	}
	ctx, stop := context.WithCancel(parent)
	stop()
	checks = checkArtifacts(ctx, p, ws)
	if len(checks) != 1 || checks[0].Passed {
		t.Fatal("ignored parent cancellation")
	}
}

func TestFailedCommandDoesNotReportNeverCalled(t *testing.T) {
	ok, failed := true, false
	p := Probe{Trace: []TraceCheck{{Tool: "run_shell", Mode: "required", Success: &ok}}}
	got := checkTrace(p, []call{{tool: "run_shell", success: &failed}})
	if len(got) != 1 || strings.Contains(got[0], "never called") {
		t.Fatal(got)
	}
}

func TestCheckEvidenceSurvivesResultSerialization(t *testing.T) {
	ws := checkWorkspaceForTest(t)
	probe := Probe{
		Verify: []Check{
			{Type: "file_absent", Path: "absent.txt"},
			{Type: "shell", Cmd: "echo first-line\necho assertion-detail\nexit 7"},
		},
		Trace:  []TraceCheck{{Tool: "read_file", Mode: "required"}},
		Answer: []AnswerCheck{{Regex: "correct", Mode: "required"}},
	}
	var result Result
	checkProbeOutcome(t.Context(), probe, ws, nil, nil, "wrong", &result)
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var saved Result
	err = json.Unmarshal(encoded, &saved)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Checks) != 2 || !saved.Checks[0].Passed || saved.Checks[0].ExitCode != nil {
		t.Fatal(saved.Checks)
	}
	failed := saved.Checks[1]
	if failed.Index != 1 || failed.Check != probe.Verify[1] || failed.Passed || failed.ExitCode == nil || *failed.ExitCode != 7 || failed.Seconds <= 0 {
		t.Fatal(failed)
	}
	if !strings.Contains(failed.Output, "first-line") || !strings.Contains(failed.Output, "assertion-detail") {
		t.Fatal("lost multiline command evidence", failed.Output)
	}
	if len(saved.ArtifactFailures) != 1 || len(saved.TraceFailures) != 1 || len(saved.AnswerFailures) != 1 || len(saved.Failures) != 3 {
		t.Fatal("outcome categories lost", saved)
	}
}
