package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
)

type toolBatch struct {
	calls              []llm.ToolCall
	results            []callResult
	executed, withheld []bool
	yielded            bool
}

// executeToolCalls validates, executes and records one assistant tool group.
// Adjacent reads overlap; mutations remain in model call order.
func (a *Agent) executeToolCalls(ctx context.Context, st *runState, step int, finishReason string, calls []llm.ToolCall, history []llm.Message) []llm.Message {
	ctx = llm.WithAttachments(ctx, a.snapshot.History, history)
	ctx = context.WithValue(ctx, contextInspectionKey{}, a.inspection)
	resolved := append([]llm.ToolCall(nil), calls...)
	results := make([]callResult, len(calls))
	for i, call := range calls {
		var err error
		resolved[i], err = a.cfg.Tools.ResolveCall(call)
		results[i].err = err
	}
	batch := toolBatch{calls: resolved, results: results}
	if a.cfg.OnProgress != nil {
		a.cfg.OnProgress(llm.Progress{Phase: "tools", Detail: fmt.Sprintf("executing %d tool calls", len(calls))})
	}
	skipped, instructions := a.validateToolCalls(resolved, finishReason, history, batch.results)
	a.runToolBatch(ctx, st, &batch, skipped)
	return a.recordToolBatch(ctx, st, &batch, finishReason, history, instructions)
}

func (a *Agent) runToolBatch(ctx context.Context, st *runState, group *toolBatch, skipped []bool) {
	calls, results := group.calls, group.results
	executed := make([]bool, len(calls))
	withheld := make([]bool, len(calls))
	yielded := false
	runOne := func(i int) {
		call := calls[i]
		if !a.cfg.Inbox.admit(&yielded) {
			withheld[i] = true
			return
		}
		if err := ctx.Err(); err != nil {
			results[i].err = err
			return
		}
		resources, err := a.callResources(call)
		if err != nil {
			results[i].err = err
			return
		}
		for _, resource := range resources {
			if !a.cfg.Inbox.admit(&yielded) {
				withheld[i] = true
				return
			}
			if err := permission.Authorize(ctx, a.cfg.Policy, a.cfg.BeforeToolCall, call.Name, resource, call.Arguments); err != nil {
				results[i].err = err
				return
			}
		}
		if !a.cfg.Inbox.admit(&yielded) {
			withheld[i] = true
			return
		}
		if err := ctx.Err(); err != nil {
			results[i].err = err
			return
		}
		executed[i] = true
		started := time.Now()
		defer func() { results[i].duration = time.Since(started) }()
		var onOutput func(string)
		if a.cfg.OnToolOutput != nil {
			onOutput = func(text string) { a.cfg.OnToolOutput(call.ID, text) }
		}
		results[i].content, results[i].err = a.cfg.Tools.RunStreaming(context.WithValue(ctx, toolCallIDKey{}, call.ID), call.Name, call.Arguments, onOutput)
	}
	notifyFinished := func(i int) {
		if executed[i] && a.cfg.OnToolFinished != nil {
			a.cfg.OnToolFinished(calls[i].ID)
		}
	}
	runBatch := func(indices []int) {
		done := make(chan int, len(indices))
		for _, i := range indices {
			go func(i int) { runOne(i); done <- i }(i)
		}
		for range indices {
			notifyFinished(<-done)
		}
	}
	var batch []int
	for i, call := range calls {
		if skipped[i] {
			continue
		}
		if a.cfg.Tools.ModeOf(call.Name) == Concurrent {
			batch = append(batch, i)
		} else {
			runBatch(batch)
			batch = nil
			runOne(i)
			notifyFinished(i)
			// A failed command may still change files. Treat it as a mutation barrier.
			if executed[i] {
				st.noteMutation()
			}
		}
	}
	runBatch(batch)

	group.executed, group.withheld, group.yielded = executed, withheld, yielded
}

type callResult struct {
	duration time.Duration
	content  string
	err      error
}
