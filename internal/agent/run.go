package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
)

// run requests a response, executes its tools and persists history until
// the model answers, the operator cancels or the step limit is reached.
func (a *Agent) run(ctx context.Context, history []llm.Message) (answer string, runErr error) {
	var st runState
	a.LastRunMutations = 0
	a.report = RunReport{MaxSteps: a.cfg.MaxSteps}
	defer func() {
		if runErr == nil {
			runErr = ctx.Err()
		}
		if runErr == nil && a.cfg.Verify != nil {
			out, err := a.cfg.Verify(ctx)
			a.report.Verification = TruncateMiddle(out, 8192)
			if err != nil {
				runErr = fmt.Errorf("verification failed: %w", err)
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
	history = a.withDirective(history)
	// Restore the task checklist when resuming a saved conversation.
	a.rehydrateTodo(ctx, history)

	for step := 0; step < a.cfg.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		resp, err := a.chat(ctx, llm.ChatRequest{
			Messages:  history,
			Tools:     a.cfg.Tools.Defs(),
			MaxTokens: a.effectiveMaxTokens(st.lastPromptTokens),
		})
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
		}
		a.report.Steps = step + 1
		a.report.LastPromptTokens = st.lastPromptTokens
		a.saveState(history)

		if len(resp.Message.ToolCalls) == 0 {
			out := a.handleFinish(&st, resp, leakedText, history)
			history = out.history
			if out.done {
				return out.answer, out.err
			}
			continue
		}
		history = a.executeToolCalls(ctx, &st, step, resp.FinishReason, resp.Message.ToolCalls, history)
		if a.maybeCompact(&history, resp.Usage, &st) {
			// The refusal text points at conversation content; compaction just
			// dropped some of it, so what it promises is no longer true.
			st.forgetIdempotentSeen()
			history = a.withDirective(history)
		}
		a.saveState(history)
		if st.repeatCount >= maxRepeatedSteps {
			from := step - st.repeatCount + 1
			a.report.Warnings = append(a.report.Warnings, fmt.Sprintf(
				"steps %d-%d issued identical tool calls; answered without tools", from, step))
			return a.forceAnswer(ctx, &st, step, history, from)
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
			history = next
			st.forgetIdempotentSeen()
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
	return llm.ChatWithRetry(ctx, a.cfg.Client, req, llm.DefaultRetryPolicy())
}

func isRetryableAPI(err error) bool {
	apiErr, ok := llm.AsAPIError(err)
	return ok && apiErr.Retryable()
}
