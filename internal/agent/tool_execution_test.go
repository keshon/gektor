package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

func TestAgent_ParallelToolCalls_PreserveOrderRegardlessOfFinishTime(t *testing.T) {
	// Two calls in one step; the FIRST one issued sleeps longer than the
	// second. If execution were sequential, the whole step would take at
	// least the sum of both delays; if parallel, roughly the max of them.
	// Delays are large enough (hundreds of ms) that runner scheduling
	// noise stays far below the 50ms gap between parallel (~400ms) and
	// sequential (~450ms): a 45ms total once failed on a loaded Windows
	// runner with 52ms of pure scheduling overhead and no signal at all.
	slow := slowToolStub{name: "slow_a", delay: 400 * time.Millisecond}
	fast := slowToolStub{name: "slow_b", delay: 50 * time.Millisecond}

	client := &twoCallClient{
		callA:        llm.ToolCall{ID: "call_a", Name: "slow_a", Arguments: json.RawMessage(`{}`)},
		callB:        llm.ToolCall{ID: "call_b", Name: "slow_b", Arguments: json.RawMessage(`{}`)},
		finalContent: "done",
	}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(slow, fast),
		System: "sys",
	})

	start := time.Now()
	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed >= slow.delay+fast.delay {
		t.Fatalf("took %v, expected well under the sum of delays (%v) if calls ran in parallel",
			elapsed, slow.delay+fast.delay)
	}

	// Order in history must match call order (call_a's result before
	// call_b's), even though call_b's tool finishes first.
	var order []string
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleTool {
			order = append(order, m.ToolCallID)
		}
	}
	if len(order) != 2 || order[0] != "call_a" || order[1] != "call_b" {
		t.Fatalf("tool result order = %v, want [call_a call_b]", order)
	}
}

func TestAgent_ExclusiveToolCalls_NeverOverlap(t *testing.T) {
	var log []string
	var mu sync.Mutex
	a1 := orderTrackingExclusiveStub{name: "a", delay: 30 * time.Millisecond, log: &log, mu: &mu}
	a2 := orderTrackingExclusiveStub{name: "b", delay: 30 * time.Millisecond, log: &log, mu: &mu}

	args, _ := json.Marshal(map[string]any{})
	client := &twoCallClient{
		callA:        llm.ToolCall{ID: "1", Name: "a", Arguments: args},
		callB:        llm.ToolCall{ID: "2", Name: "b", Arguments: args},
		finalContent: "done",
	}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(a1, a2),
		System: "sys",
	})

	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// If they overlapped, we'd see start:a, start:b, end:a, end:b (or
	// similar interleaving). Strictly sequential means each end comes
	// immediately after its own start, with no other start in between.
	if len(log) != 4 {
		t.Fatalf("expected 4 log entries, got %d: %v", len(log), log)
	}
	if !((log[0] == "start:a" && log[1] == "end:a" && log[2] == "start:b" && log[3] == "end:b") ||
		(log[0] == "start:b" && log[1] == "end:b" && log[2] == "start:a" && log[3] == "end:a")) {
		t.Fatalf("exclusive calls overlapped, got order: %v", log)
	}
}

func TestAgent_MixedConcurrentAndExclusive_ExclusiveRunsAfterConcurrentBatch(t *testing.T) {
	var log []string
	var mu sync.Mutex
	concurrentTool := slowToolStub{name: "fast_read", delay: 10 * time.Millisecond}
	exclusiveTool := orderTrackingExclusiveStub{name: "write", delay: 10 * time.Millisecond, log: &log, mu: &mu}

	args, _ := json.Marshal(map[string]any{})
	client := &twoCallClient{
		callA:        llm.ToolCall{ID: "1", Name: "fast_read", Arguments: args},
		callB:        llm.ToolCall{ID: "2", Name: "write", Arguments: args},
		finalContent: "done",
	}
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(concurrentTool, exclusiveTool),
		System: "sys",
	})

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "done" {
		t.Fatalf("result = %q, want %q", out, "done")
	}
	// The Exclusive tool must have actually run (both log entries present).
	if len(log) != 2 || log[0] != "start:write" || log[1] != "end:write" {
		t.Fatalf("expected exclusive tool to run cleanly, got log: %v", log)
	}
}

func TestToolBatchKeepsMixedResultsPairedInModelOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	release := make(chan struct{})
	slow := steeringTool{"slow", Concurrent, func(toolContext context.Context) (string, error) {
		select {
		case <-release:
			return "slow result", nil
		case <-toolContext.Done():
			return "", toolContext.Err()
		}
	}}
	fast := steeringTool{"fast", Concurrent, func(context.Context) (string, error) {
		return "partial fast result", errors.New("fast failure")
	}}
	client := &stubClient{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: steeringCalls("slow", "unknown", "fast")}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}

	var finished []string
	var results []ToolResult
	a := New(Config{
		Client: client,
		Tools:  NewRegistry(slow, fast),
		OnToolFinished: func(id string) {
			finished = append(finished, id)
			if id == "fast" {
				close(release)
			}
		},
		OnResult: func(result ToolResult) { results = append(results, result) },
	})
	_, err := a.Run(ctx, "inspect")
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(finished, []string{"fast", "slow"}) {
		t.Fatalf("completion order = %v", finished)
	}
	if len(results) != 3 {
		t.Fatalf("results = %+v", results)
	}
	if results[0].CallID != "slow" || results[0].Output != "slow result" || results[0].Failed {
		t.Fatalf("slow result = %+v", results[0])
	}
	if results[1].CallID != "unknown" || !results[1].Failed || results[1].Duration != 0 {
		t.Fatalf("rejected result = %+v", results[1])
	}
	if results[2].CallID != "fast" || !results[2].Failed || !strings.Contains(results[2].Output, "partial fast result\nerror: fast failure") {
		t.Fatalf("failed execution result = %+v", results[2])
	}
	if a.Report().ToolsExecuted != 2 {
		t.Fatalf("executed = %d, want 2", a.Report().ToolsExecuted)
	}

	var recorded []string
	for _, msg := range client.lastHistory {
		if msg.Role == llm.RoleTool {
			recorded = append(recorded, msg.ToolCallID)
		}
	}
	if !reflect.DeepEqual(recorded, []string{"slow", "unknown", "fast"}) {
		t.Fatalf("history order = %v", recorded)
	}
}
