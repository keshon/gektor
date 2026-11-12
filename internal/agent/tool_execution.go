package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/permission"
)

// Workers update separate entries; recording starts after every worker finishes.
// The inbox mutex guards yielded while calls are being admitted.
type toolBatch struct {
	entries []toolExecution
	yielded bool
}

type toolExecution struct {
	call     llm.ToolCall
	content  string
	err      error
	duration time.Duration
	executed bool
	withheld bool
}

// executeToolCalls validates, executes and records one assistant tool group.
// Adjacent reads overlap; mutations remain in model call order.
func (a *Agent) executeToolCalls(ctx context.Context, st *runState, step int, finishReason string, calls []llm.ToolCall, history []llm.Message) []llm.Message {
	ctx = llm.WithAttachments(ctx, a.snapshot.History, history)
	ctx = context.WithValue(ctx, contextInspectionKey{}, a.inspection)
	batch := toolBatch{entries: make([]toolExecution, len(calls))}
	for i, call := range calls {
		entry := &batch.entries[i]
		entry.call = call
	}

	if a.cfg.OnProgress != nil {
		a.cfg.OnProgress(llm.Progress{Phase: "tools", Detail: fmt.Sprintf("executing %d tool calls", len(calls))})
	}
	instructions := a.validateToolCalls(batch.entries, finishReason, history)
	a.runToolBatch(ctx, st, &batch)
	return a.recordToolBatch(ctx, st, &batch, finishReason, history, instructions)
}

func (a *Agent) runToolBatch(ctx context.Context, st *runState, batch *toolBatch) {
	runOne := func(i int) {
		entry := &batch.entries[i]
		call := entry.call
		if !a.cfg.Inbox.admit(&batch.yielded) {
			entry.withheld = true
			return
		}
		err := ctx.Err()
		if err != nil {
			entry.err = err
			return
		}

		resources, err := a.callResources(call)
		if err != nil {
			entry.err = err
			return
		}

		for _, resource := range resources {
			if !a.cfg.Inbox.admit(&batch.yielded) {
				entry.withheld = true
				return
			}
			err = permission.Authorize(ctx, a.cfg.Policy, a.cfg.BeforeToolCall, call.Name, resource, call.Arguments)
			if err != nil {
				entry.err = err
				return
			}
		}

		if !a.cfg.Inbox.admit(&batch.yielded) {
			entry.withheld = true
			return
		}
		err = ctx.Err()
		if err != nil {
			entry.err = err
			return
		}

		entry.executed = true
		started := time.Now()
		defer func() { entry.duration = time.Since(started) }()
		var onOutput func(string)
		if a.cfg.OnToolOutput != nil {
			onOutput = func(text string) { a.cfg.OnToolOutput(call.ID, text) }
		}

		callContext := context.WithValue(ctx, toolCallIDKey{}, call.ID)
		entry.content, entry.err = a.cfg.Tools.RunStreaming(callContext, call.Name, call.Arguments, onOutput)
	}

	notifyFinished := func(i int) {
		entry := &batch.entries[i]
		if entry.executed && a.cfg.OnToolFinished != nil {
			a.cfg.OnToolFinished(entry.call.ID)
		}
	}

	runBatch := func(indices []int) {
		done := make(chan int, len(indices))
		for _, i := range indices {
			go func(i int) {
				runOne(i)
				done <- i
			}(i)
		}
		for range indices {
			notifyFinished(<-done)
		}
	}

	var concurrent []int
	for i := range batch.entries {
		entry := &batch.entries[i]
		if entry.err != nil {
			continue
		}
		if a.cfg.Tools.ModeOf(entry.call.Name) == Concurrent {
			concurrent = append(concurrent, i)
			continue
		}

		runBatch(concurrent)
		concurrent = nil
		runOne(i)
		notifyFinished(i)

		// A failed command may still change files. Treat it as a mutation barrier.
		if entry.executed {
			st.noteMutation()
		}
	}
	runBatch(concurrent)
}
