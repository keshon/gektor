package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

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
	if failures := checkArtifacts(parent, p, ws); len(failures) != 0 {
		t.Fatal(failures)
	}
	ctx, stop := context.WithCancel(parent)
	stop()
	if failures := checkArtifacts(ctx, p, ws); len(failures) == 0 {
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
