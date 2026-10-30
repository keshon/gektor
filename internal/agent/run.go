package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/session"
)

// run requests a response, executes its tools and persists history until
// the model answers, the operator cancels or the step limit is reached.
func (a *Agent) run(ctx context.Context, history []llm.Message) (answer string, runErr error) {
	// Derive both deadlines from the enriched context: tools execute on workCtx,
	// so enriching only the hard-deadline context loses vision/output metadata.
	if a.cfg.ToolContext != nil {
		ctx = a.cfg.ToolContext(ctx)
	}
	ctx, workCtx, cancelBudget := runBudget(ctx, a.cfg.RunTimeout)
	defer cancelBudget()
	if err := a.cfg.Tools.Err(); err != nil {
		return "", fmt.Errorf("tool registry: %w", err)
	}
	if err := a.cfg.Policy.Validate(); err != nil {
		return "", err
	}
	if a.cfg.RestoreBrowser != nil {
		if err := a.cfg.RestoreBrowser(a.snapshot.Browser); err != nil {
			return "", fmt.Errorf("restore browser profile: %w", err)
		}
	}
	if a.cfg.RestoreCapabilities != nil {
		if err := a.cfg.RestoreCapabilities(workCtx, a.snapshot.Capabilities); err != nil {
			return "", fmt.Errorf("restore capabilities: %w", err)
		}
	}
	var st runState
	runStart := len(a.snapshot.History)
	if runStart == 2 {
		runStart = 1
	}
	hasRequest := false
	for _, msg := range history[a.checkpointLen:] {
		if msg.Role == llm.RoleUser {
			hasRequest = true
			break
		}
	}
	if !hasRequest {
		runStart = 1
		if len(a.snapshot.Runs) > 0 {
			runStart = a.snapshot.Runs[len(a.snapshot.Runs)-1].Start
		}
	}
	if n := len(a.snapshot.History); hasRequest && n > 0 && len(a.snapshot.History[n-1].ToolCalls) > 0 {
		runStart = n - 1
	}
	a.LastRunMutations = 0
	a.report = RunReport{MaxSteps: a.cfg.MaxSteps}
	a.cfg.Inbox.start()
	history = a.withDirective(history)
	a.saveState(history)
	defer func() {
		if a.cfg.RunTimeout > 0 && errors.Is(workCtx.Err(), context.DeadlineExceeded) && runErr != nil {
			a.closeBudget(ctx, &st, history)
			runErr = ErrRunBudget
			if errors.Is(ctx.Err(), context.Canceled) {
				runErr = ctx.Err()
			}
		}
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
		if errors.Is(runErr, ErrRunBudget) {
			a.report.Outcome = "incomplete"
		}
		if a.cfg.StateFile != "" {
			a.snapshot.Runs = append(a.snapshot.Runs, session.Run{Start: runStart, End: len(a.snapshot.History), Outcome: a.report.Outcome, Steps: a.report.Steps, Verification: a.report.Verification})
			if err := a.snapshot.Save(a.cfg.StateFile); err != nil {
				a.report.Warnings = append(a.report.Warnings, "run boundary persistence failed: "+err.Error())
			}
		}
	}()
	if a.cfg.BeginChanges != nil {
		var err error
		st.observeChanges, err = a.cfg.BeginChanges(workCtx)
		if err != nil {
			return "", fmt.Errorf("observe workspace: %w", err)
		}
	}
	// Restore the task checklist when resuming a saved conversation.
	a.rehydrateTodo(ctx, history)
	if a.cfg.RestoreTools != nil {
		if err := a.cfg.RestoreTools(workCtx, a.snapshot.History); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			history = a.nudge(history, NudgeRecovery, "Optional session tools unavailable: "+err.Error()+". Continue with available tools or explicitly retry activation.")
		}
	}

	for step := 0; step < a.cfg.MaxSteps; step++ {
		if err := workCtx.Err(); err != nil {
			return "", err
		}
		history = a.acceptInputs(history, a.cfg.Inbox.take(false), &st)
		history = a.acceptNotifications(ctx, history)
		history = a.withCapabilityInstructions(history)
		req := llm.ChatRequest{
			Messages: a.reuseContext(history),
			Tools:    a.cfg.Tools.Defs(),
		}
		estimate := llm.EstimatePromptTokens(req)
		promptTokens := st.projectPrompt(estimate)
		if a.maybeCompact(&history, llm.Usage{PromptTokens: promptTokens}, &st, workCtx) {
			st.lastPromptEstimate = 0
			history = a.withDirective(history)
			history = a.withCapabilityInstructions(history)
			req.Messages = a.reuseContext(history)
			estimate = llm.EstimatePromptTokens(req)
			promptTokens = estimate
			a.saveState(history)
		}
		if st.compactErr != nil {
			return "", fmt.Errorf("checkpoint failed; previous context retained: %w", st.compactErr)
		}
		req.MaxTokens = a.effectiveMaxTokens(promptTokens)
		st.lastPromptEstimate = estimate
		a.report.Steps = step + 1
		resp, err := a.chat(workCtx, req)
		if err != nil {
			if len(resp.Message.ToolCalls) > 0 || resp.Message.Content != "" || resp.Message.Reasoning != "" {
				for i := range resp.Message.ToolCalls {
					resp.Message.ToolCalls[i].InputError = "incomplete_stream"
				}
				resp.Message.FinishReason = resp.FinishReason
				message := recordableMessage(resp.Message)
				if a.cfg.OnStep != nil {
					a.cfg.OnStep(step, message)
				}
				history = append(history, message)
				if len(resp.Message.ToolCalls) > 0 {
					history = a.executeToolCalls(workCtx, &st, step, "incomplete_stream", resp.Message.ToolCalls, history)
				} else {
					history = a.nudge(history, "incomplete_stream", "Generation stream was interrupted; the preceding answer is incomplete. No tool call from that interrupted response was executed.")
				}
				a.saveState(history)
				return "", fmt.Errorf("step %d: incomplete response retained: %w", step, err)
			}
			history, err = a.handleChatError(&st, step, err, history, workCtx)
			if err != nil {
				return "", err
			}
			continue
		}
		resp, leakedText := a.recoverLeakedCalls(resp, step)
		resp.Message.FinishReason = resp.FinishReason
		message := recordableMessage(resp.Message)
		if a.cfg.OnStep != nil {
			a.cfg.OnStep(step, message)
		}
		if a.cfg.OnUsage != nil {
			a.cfg.OnUsage(step, resp.Usage)
		}
		history = append(history, message)
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
					if next := a.acceptNotifications(ctx, history); len(next) > len(history) {
						history = next
						a.report.Final = ""
						continue
					}
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
		history = a.executeToolCalls(workCtx, &st, step, resp.FinishReason, resp.Message.ToolCalls, history)
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
			answer, err := a.forceAnswer(workCtx, &st, step, history, from)
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

func (a *Agent) acceptNotifications(ctx context.Context, history []llm.Message) []llm.Message {
	if a.cfg.Notifications == nil {
		return history
	}
	for _, note := range a.cfg.Notifications(ctx) {
		if a.cfg.LimitOutput != nil {
			note = a.cfg.LimitOutput(ctx, "background", note, 8192)
		}
		history = a.nudge(history, "background", note)
	}
	return history
}

func (a *Agent) handleChatError(st *runState, step int, err error, history []llm.Message, contexts ...context.Context) ([]llm.Message, error) {
	if !llm.IsOverflow(err) {
		return nil, fmt.Errorf("step %d: chat: %w", step, err)
	}
	shrunk := false
	hasExchange := false
	for _, message := range history {
		if message.Role == llm.RoleAssistant || message.Role == llm.RoleTool {
			hasExchange = true
			break
		}
	}
	if hasExchange {
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
			a.captureHistory(history)
			var compactErr error
			next, compactErr = retainArchivedInstructions(a.snapshot, next)
			if compactErr == nil && a.cfg.Summarize {
				ctx := context.Background()
				if len(contexts) > 0 {
					ctx = contexts[0]
				}
				next, compactErr = a.checkpoint(ctx, history, next)
			}
			if compactErr != nil {
				return history, fmt.Errorf("overflow recovery checkpoint failed; original archive and context retained: %w", compactErr)
			}
			a.compactContext(history, next)
			history = next
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
	notice := prompts.OverflowRecovered
	if !hasExchange {
		notice = "The backend reported context overflow before any completed exchanges. There is no earlier conversation to compact. Retrying once with the original instructions; a repeated overflow will stop the run."
	}
	history = a.nudge(history, NudgeOverflow, notice)
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
func (a *Agent) chat(ctx context.Context, req llm.ChatRequest) (response llm.ChatResponse, callErr error) {
	a.requests++
	for i := range req.Messages {
		m := &req.Messages[i]
		if m.Harness == "" && strings.HasPrefix(m.Content, harnessPrefix) {
			m.Harness = "notice"
		}
		if m.Harness != "" && m.Request == 0 {
			m.Request = a.requests
			if a.cfg.OnContext != nil {
				a.cfg.OnContext(*m)
			}
		}
	}
	a.saveState(req.Messages)
	req.Messages = llm.ModelMessages(req.Messages)
	breakdown := llm.AnalyzeContext(req, a.cfg.SystemSections)
	breakdown.Request = a.requests
	a.report.Prompt = breakdown
	if a.cfg.OnPrompt != nil {
		a.cfg.OnPrompt(breakdown)
	}
	defer func() {
		if response.Usage.PromptTokens > 0 {
			breakdown.BackendTokens, breakdown.CachedTokens, breakdown.BackendEstimated = response.Usage.PromptTokens, response.Usage.CachedTokens, response.Usage.Estimated
			a.report.Prompt = breakdown
			if a.cfg.OnPrompt != nil {
				a.cfg.OnPrompt(breakdown)
			}
		}
	}()
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
			case !shown && len(resp.Message.ToolCalls) == 0 && (errors.Is(err, llm.ErrIncompleteStream) || llm.IsRetryableTransport(err)):
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
