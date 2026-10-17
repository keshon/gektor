package agent

import (
	"context"
	"fmt"
	"errors"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
)

// run is the single authoritative execution loop: one model turn per
// iteration, dispatched to finish handling (text answers) or tool
// execution (tool calls). Stage functions own their details — this
// function owns only the order: chat, recover, bookkeeping, dispatch,
// interject, persist. See handleFinish (finish.go) and executeToolCalls
// (tools.go).
func (a *Agent) run(ctx context.Context, history []llm.Message) (answer string, runErr error) {
	var st runState
	a.LastRunMutations = 0
	a.report = RunReport{MaxSteps: a.cfg.MaxSteps}
	defer func() {
		a.LastRunMutations = st.mutatingSucceeded
		a.report.MutatedPaths = st.mutatedPaths
		if runErr == nil && st.observationErr != nil {
			answer = ""
			runErr = fmt.Errorf("cannot verify workspace effects: %w", st.observationErr)
		}
		a.report.Outcome = "completed"
		if runErr != nil {
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
	// The registry is fresh per run, but the checklist is per task:
	// restore it from the last todo call in history, or the todo-gated
	// finish and todo funding only ever see same-turn todos — a resumed
	// session with open todos would finish without a bounce.
	a.rehydrateTodo(ctx, history)

	for step := 0; step < a.cfg.MaxSteps+st.todoFunded; step++ {
		if st.observationErr != nil {
			return "", fmt.Errorf("observe workspace: %w", st.observationErr)
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
		// Usage rides alongside OnStep rather than inside it: changing
		// OnStep's signature would break every caller (roles, mission,
		// eval, CLI) for data only some observers want.
		if a.cfg.OnUsage != nil {
			a.cfg.OnUsage(step, resp.Usage)
		}
		history = append(history, resp.Message)
		st.toolCalls += len(resp.Message.ToolCalls)
		budgetNudge := a.budgetWarning(resp.Usage, &st.warnedThreshold)
		if budgetNudge == "" {
			// Token notices win ties through the shared slot: context
			// death strands more work than step death, and one signal
			// per step is the whole point of interject.
			budgetNudge = a.stepWarning(&st, step)
		}
		if resp.Usage.PromptTokens > 0 {
			st.lastPromptTokens = resp.Usage.PromptTokens
		}
		a.report.Steps = step + 1
		a.report.LastPromptTokens = st.lastPromptTokens
		a.maybeCompact(&history, resp.Usage, &st)
		a.saveState(history)

		if len(resp.Message.ToolCalls) == 0 {
			a.observeWorkspace(&st)
			if st.observationErr != nil {
				return "", fmt.Errorf("observe workspace: %w", st.observationErr)
			}
			out := a.handleFinish(ctx, &st, step, resp, leakedText, budgetNudge, history)
			history = out.history
			if out.done {
				return out.answer, out.err
			}
			continue
		}
		repeat, newHistory := a.executeToolCalls(ctx, &st, step, resp.FinishReason, resp.Message.ToolCalls, history)
		history = newHistory
		if msg := a.interject(&st, step, stepOutcome{repeat: repeat, budget: budgetNudge}); msg != "" {
			history = append(history, llm.Message{Role: llm.RoleUser, Content: msg})
		}
		a.saveState(history)
	}

	// LastRunMutations and report.MutatedPaths are set by the defer above,
	// which owns them for every exit path.
	if _, open := a.cfg.Tools.TodoProgress(); len(open) > 0 {
		a.report.OpenTodos = open
	}
	if wrap, ok := a.wrapUpTurn(ctx, &st, history); ok {
		a.report.WrapUp = wrap
	}
	return "", fmt.Errorf("%w (%d) without finishing", ErrMaxSteps, a.cfg.MaxSteps+st.todoFunded)
}

// handleChatError recovers from context overflow by compacting history
// and continuing with a nudge; anything else is infrastructure —
// mission treats it as resumable errInfra. Returns the history to
// continue with, or a fatal error.
//
// Exactly one compact-and-retry per run: the first overflow compacts
// aggressively (keep/2) and continues; a second overflow — or one where
// compaction shrank nothing — fails fast with directions. Retrying past
// that point burns one backend call per step until MaxSteps, every
// attempt failing identically.
func (a *Agent) handleChatError(st *runState, step int, err error, history []llm.Message) ([]llm.Message, error) {
	if !llm.IsOverflow(err) {
		return nil, fmt.Errorf("step %d: chat: %w", step, err)
	}
	shrunk := false
	if len(history) > 2 {
		keep := a.cfg.CompactKeepSteps
		if keep <= 0 {
			keep = DefaultCompactKeepSteps
		}
		// Caches drop only when history actually shrank: the
		// refusal texts cite content that must really be gone.
		if next := compactHistory(history, keep/2); len(next) < len(history) {
			history = next
			dropRepeatCaches(st)
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

// recoverLeakedCalls extracts well-formed tool calls a model wrote as
// text instead of making structured ones. Recovered calls run through
// the normal tool path below - guards, permissions, repeat detection
// and all. Unparseable remainders keep the nudge path in the
// no-tool-calls branch; the returned verdict carries that so the branch
// does not re-detect. Length-truncated stumps skip recovery: a cutoff
// generation is not a hidden call.
func (a *Agent) recoverLeakedCalls(resp llm.ChatResponse, step int) (llm.ChatResponse, bool) {
	var leakedText bool
	if len(resp.Message.ToolCalls) == 0 && resp.FinishReason != "length" {
		known := map[string]bool{}
		if a.cfg.Tools != nil {
			for _, n := range a.cfg.Tools.Names() {
				known[n] = true
			}
		}
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

// stepWarning renders a pacing notice at 75%/90% of the enforced step
// budget (base plus todo-funded extension), each once per run. The
// limit moves as funding lands; the latches don't move back, so a jump
// past a threshold still fires it exactly once. Unconditional on
// TodoFunding — pacing information is useful whether or not the budget
// can grow.
func (a *Agent) stepWarning(st *runState, step int) string {
	limit := a.cfg.MaxSteps + st.todoFunded
	if limit <= 0 {
		return ""
	}
	used := step + 1
	pct := used * 100 / limit
	left := limit - used
	switch {
	case pct >= 90 && st.warnedSteps < 90:
		st.warnedSteps = 90
		return fmt.Sprintf(prompts.StepWarning, used, limit, pct, left)
	case pct >= 75 && st.warnedSteps < 75:
		st.warnedSteps = 75
		return fmt.Sprintf(prompts.StepNotice, used, limit, pct, left)
	default:
		return ""
	}
}

// stepTag renders the live budget counter appended to decision-point
// messages the model demonstrably reads (verify, stuck escalation,
// refusals). Numbers, not prose, so it lives here rather than in a
// prompt template: one format everywhere, computed at the call site.
func (a *Agent) stepTag(st *runState, step int) string {
	limit := a.cfg.MaxSteps + st.todoFunded
	if limit <= 0 {
		return ""
	}
	used := step + 1
	return fmt.Sprintf(" (step %d/%d, %d left)", used, limit, limit-used)
}
