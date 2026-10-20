package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

// run requests a response, executes its tools and persists history until
// the model answers, the operator cancels or the step limit is reached.
func (a *Agent) run(ctx context.Context, history []llm.Message) (answer string, runErr error) {
	var st runState
	a.LastRunMutations = 0
	a.report = RunReport{MaxSteps: a.cfg.MaxSteps}
	a.cfg.Inbox.start()
	history = a.withDirective(history)
	a.saveState(history)
	defer func() {
		// Even when cancelled or failed, retain accepted steering for retry.
		if pending := a.cfg.Inbox.stop(); len(pending) > 0 {
			history = a.acceptInputs(a.snapshot.Context(), pending, &st)
		}
		if runErr == nil {
			runErr = ctx.Err()
		}
		if runErr == nil && a.cfg.Verify != nil {
			out, err := a.cfg.Verify(ctx)
			a.report.Verification = TruncateMiddle(out, 8192)
			if err != nil {
				runErr = fmt.Errorf("verification failed: %w", err)
				// A user-triggered retry needs the measured failure, not only
				// the preceding final answer. Persist it without requesting
				// another model turn automatically.
				if ctx.Err() == nil {
					history = a.nudge(history, NudgeVerification, fmt.Sprintf("Verification failed: %v\n%s", err, a.report.Verification))
					a.saveState(history)
				}
			}
		}
		a.observeWorkspace(&st)
		if runErr == nil {
			runErr = ctx.Err()
		}
		a.LastRunMutations = st.mutatingSucceeded
		a.report.MutatedPaths = st.mutatedPaths
		_, a.report.OpenTodos = a.cfg.Tools.TodoProgress()
		if st.observationErr != nil {
			a.report.Warnings = append(a.report.Warnings, "workspace observation failed: "+st.observationErr.Error())
		}
		if runErr == nil && st.observationErr != nil {
			answer = ""
			runErr = fmt.Errorf("cannot verify workspace effects: %w", st.observationErr)
		}
		a.report.Outcome = "completed"
		if runErr != nil {
			answer = ""
			a.report.Outcome = "failed"
			a.report.Final = ""
		}
		if ctx.Err() != nil {
			a.report.Outcome = "cancelled"
		}
	}()
	if a.cfg.BeginChanges != nil {
		var err error
		st.observeChanges, err = a.cfg.BeginChanges(ctx)
		if err != nil {
			return "", fmt.Errorf("observe workspace: %w", err)
		}
	}
	// Restore the task checklist when resuming a saved conversation.
	a.rehydrateTodo(ctx, history)

	for step := 0; step < a.cfg.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		history = a.acceptInputs(history, a.cfg.Inbox.take(false), &st)
		req := llm.ChatRequest{
			Messages: history,
			Tools:    a.cfg.Tools.Defs(),
		}
		estimate := llm.EstimatePromptTokens(req)
		promptTokens := st.projectPrompt(estimate)
		if a.maybeCompact(&history, llm.Usage{PromptTokens: promptTokens}, &st) {
			st.forgetIdempotentSeen()
			st.lastPromptEstimate = 0
			history = a.withDirective(history)
			req.Messages = history
			estimate = llm.EstimatePromptTokens(req)
			promptTokens = estimate
			a.saveState(history)
		}
		req.MaxTokens = a.effectiveMaxTokens(promptTokens)
		st.lastPromptEstimate = estimate
		resp, err := a.chat(ctx, req)
		if err != nil {
			history, err = a.handleChatError(&st, step, err, history)
			if err != nil {
				return "", err
			}
			continue
		}
		resp, leakedText := a.recoverLeakedCalls(resp, step)
		if a.cfg.OnStep != nil {
			a.cfg.OnStep(step, resp.Message)
		}
		if a.cfg.OnUsage != nil {
			a.cfg.OnUsage(step, resp.Usage)
		}
		history = append(history, resp.Message)
		if resp.Usage.PromptTokens > 0 {
			st.lastPromptTokens = resp.Usage.PromptTokens
		} else {
			st.lastPromptTokens = estimate
		}
		a.report.Steps = step + 1
		a.report.LastPromptTokens = st.lastPromptTokens
		a.saveState(history)

		if len(resp.Message.ToolCalls) == 0 {
			out := a.handleFinish(&st, resp, leakedText, history)
			history = out.history
			if out.done {
				if out.err == nil {
					if pending := a.cfg.Inbox.take(true); len(pending) > 0 {
						history = a.acceptInputs(history, pending, &st)
						a.report.Final = ""
						continue
					}
				}
				return out.answer, out.err
			}
			a.saveState(history)
			continue
		}
		history = a.executeToolCalls(ctx, &st, step, resp.FinishReason, resp.Message.ToolCalls, history)
		if remaining := a.cfg.MaxSteps - step - 1; remaining == 2 {
			history = a.nudge(history, "step_limit", "2 model steps remain in this run, including the final answer. Finish the task or report concrete progress and remaining work.")
		}
		a.saveState(history)
		if a.cfg.Inbox.Len() > 0 {
			continue
		}
		if st.repeatCount >= maxRepeatedSteps {
			from := step - st.repeatCount + 1
			a.report.Warnings = append(a.report.Warnings, fmt.Sprintf(
				"steps %d-%d issued identical tool calls; answered without tools", from, step))
			answer, err := a.forceAnswer(ctx, &st, step, history, from)
			if err == nil && a.cfg.Inbox != nil {
				// The tool-free answer is also an in-flight model request.
				// It must observe the same completion boundary as normal answers.
				history = a.snapshot.Context()
				if pending := a.cfg.Inbox.take(true); len(pending) > 0 {
					history = a.acceptInputs(history, pending, &st)
					a.report.Final = ""
					step++ // forceAnswer used an additional model round-trip.
					continue
				}
			}
			return answer, err
		}
	}

	// LastRunMutations and report.MutatedPaths are set by the defer above,
	// which owns them for every exit path.
	return "", fmt.Errorf("%w (%d) without finishing", ErrMaxSteps, a.cfg.MaxSteps)
}

func (a *Agent) handleChatError(st *runState, step int, err error, history []llm.Message) ([]llm.Message, error) {
	if !llm.IsOverflow(err) {
		return nil, fmt.Errorf("step %d: chat: %w", step, err)
	}
	shrunk := false
	if len(history) > 2 {
		keep := a.cfg.CompactKeepSteps
		if keep <= 0 {
			// Overrides a disabled threshold: the backend has already said
			// the prompt does not fit, so this is recovery rather than
			// housekeeping, and refusing would just end the run.
			keep = DefaultCompactKeepSteps
		}
		// Caches drop only when history actually shrank: the
		// refusal texts cite content that must really be gone.
		if next := compactHistory(history, keep/2); len(next) < len(history) {
			a.compactContext(history, next)
			history = next
			st.forgetIdempotentSeen()
			st.lastPromptEstimate = 0
			history = a.withDirective(history)
			shrunk = true
		}
		a.saveState(history)
	} else {
		// Nothing to compact: a fresh run's history is the system
		// prompt and the task, and dropping either would lose the
		// instructions. The overflow may be synthetic (an injected
		// probe) or a backend that rejects the request for another
		// reason; retry once and fail fast if it recurs, which is
		// what st.overflowRetried guards against.
		shrunk = true
	}
	if !shrunk || st.overflowRetried {
		return nil, fmt.Errorf("step %d: context overflow persists after compaction — /compact the session, shorten the task, or use a larger-context model: %w", step, err)
	}
	st.overflowRetried = true
	history = a.nudge(history, NudgeOverflow, prompts.OverflowRecovered)
	a.saveState(history)
	return history, nil
}

func (a *Agent) recoverLeakedCalls(resp llm.ChatResponse, step int) (llm.ChatResponse, bool) {
	var leakedText bool
	if len(resp.Message.ToolCalls) == 0 && resp.FinishReason != "length" {
		known := a.knownTools()
		var recovered []llm.LeakedCall
		var cleaned string
		recovered, cleaned, leakedText = llm.ExtractLeakedCalls(resp.Message.Content, known)
		for i, rc := range recovered {
			resp.Message.ToolCalls = append(resp.Message.ToolCalls, llm.ToolCall{
				ID:        llm.LeakCallID(step, i),
				Name:      rc.Name,
				Arguments: rc.Arguments,
			})
		}
		resp.Message.Content = cleaned
	}
	return resp, leakedText
}

// chat runs one model turn: streaming when configured and supported,
// otherwise ChatWithRetry. The stream is retried unary only while nothing
// has been shown yet, since a partial stream cannot be resumed.
func (a *Agent) chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	req.Temperature = a.cfg.Temperature
	req.OnProgress = a.cfg.OnProgress
	progress := llm.Progress{Phase: "waiting", PromptTokens: llm.EstimatePromptTokens(req), Estimated: true}
	if req.OnProgress != nil {
		req.OnProgress(progress)
	}
	if a.cfg.Stream {
		if s, ok := a.cfg.Client.(llm.Streamer); ok {
			shown := false
			resp, err := s.Stream(ctx, req, func(r string) {
				// Reasoning counts as shown. It reaches the screen too,
				// so a stream that dies after thinking but before
				// answering would otherwise be retried into a duplicate
				// THINK block.
				shown = true
				if a.cfg.OnReasoning != nil {
					a.cfg.OnReasoning(r)
				}
			}, func(d string) {
				shown = true
				if a.cfg.OnDelta != nil {
					a.cfg.OnDelta(d)
				}
			})
			switch {
			case err == nil:
				return resp, nil
			case errors.Is(err, llm.ErrStreamUnsupported):
				// Fall through to the unary path.
			case !shown && (errors.Is(err, llm.ErrIncompleteStream) || llm.IsRetryableTransport(err)):
				// Nothing reached the screen, so a unary retry is safe.
			case !shown && isRetryableAPI(err):
				// An HTTP error status arrives before any SSE data, so
				// nothing streamed and there is nothing to duplicate.
			default:
				return resp, err
			}
		}
	}
	if req.OnProgress != nil {
		progress.Phase = "unary"
		req.OnProgress(progress)
	}
	return llm.ChatWithRetry(ctx, a.cfg.Client, req, llm.DefaultRetryPolicy())
}

func isRetryableAPI(err error) bool {
	apiErr, ok := llm.AsAPIError(err)
	return ok && apiErr.Retryable()
}
