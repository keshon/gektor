package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// finalizeRun retains accepted work before verification and outcome persistence.
// It runs before the budget is cancelled on every initialized run exit.
func (a *Agent) finalizeRun(ctx, workCtx context.Context, st *runState, history []llm.Message, runStart int, answer string, runErr error) (string, error) {
	if a.cfg.RunTimeout > 0 && errors.Is(workCtx.Err(), context.DeadlineExceeded) && runErr != nil {
		a.closeBudget(ctx, st, history)
		runErr = ErrRunBudget
		if errors.Is(ctx.Err(), context.Canceled) {
			runErr = ctx.Err()
		}
	}

	// Even when cancelled or failed, retain accepted steering for retry.
	pending := a.cfg.Inbox.stop()
	if len(pending) > 0 {
		history = a.acceptInputs(a.snapshot.Context(), pending, st)
	}
	ctxErr := ctx.Err()
	if ctxErr != nil && (runErr == nil || errors.Is(runErr, ErrMaxSteps)) {
		// Cancellation during the last tool call is not step exhaustion.
		runErr = ctxErr
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
				message := fmt.Sprintf("Verification failed: %v\n%s", err, a.report.Verification)
				history = a.nudge(history, NudgeVerification, message)
				a.saveState(history)
			}
		}
	}

	a.observeWorkspace(ctx, st)
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
		run := conversation.Run{
			Start:        runStart,
			End:          len(a.snapshot.History),
			Outcome:      a.report.Outcome,
			Steps:        a.report.Steps,
			Verification: a.report.Verification,
			Error:        conversation.FailureText(runErr),
		}
		a.snapshot.Runs = append(a.snapshot.Runs, run)
		err := a.snapshot.Save(a.cfg.StateFile)
		if err != nil {
			a.report.Warnings = append(a.report.Warnings, "run boundary persistence failed: "+err.Error())
		}
	}

	return answer, runErr
}

// restoreRun validates wiring and restores saved selections before opening a run.
func (a *Agent) restoreRun(ctx context.Context) error {
	err := a.cfg.Tools.Err()
	if err != nil {
		return fmt.Errorf("tool registry: %w", err)
	}
	err = a.cfg.Policy.Validate()
	if err != nil {
		return err
	}

	if a.cfg.Browser != nil {
		err = a.cfg.Browser.SelectProfile(a.snapshot.Browser)
		if err != nil {
			return fmt.Errorf("restore browser profile: %w", err)
		}
	}
	if a.cfg.Catalog != nil {
		err = a.cfg.Catalog.Restore(ctx, a.snapshot.Capabilities)
		if err != nil {
			return fmt.Errorf("restore capabilities: %w", err)
		}
	}

	return nil
}

// startRun records the archive boundary before accepting tools or steering.
func (a *Agent) startRun(history []llm.Message) ([]llm.Message, int) {
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
	n := len(a.snapshot.History)
	if hasRequest && n > 0 && len(a.snapshot.History[n-1].ToolCalls) > 0 {
		runStart = n - 1
	}

	a.LastRunMutations = 0
	a.report = RunReport{MaxSteps: a.cfg.MaxSteps, Archive: a.cfg.StateFile}
	a.cfg.Inbox.start()
	history = a.withDirective(history)
	a.saveState(history)

	return history, runStart
}

// prepareRun may fail after initialization; finalization still owns that exit.
func (a *Agent) prepareRun(ctx, workCtx context.Context, st *runState, history []llm.Message) ([]llm.Message, error) {
	if a.cfg.Changes != nil {
		err := a.cfg.Changes.Begin(workCtx)
		if err != nil {
			return history, fmt.Errorf("observe workspace: %w", err)
		}
		st.observing = true
	}

	// Restore the task checklist when resuming a saved conversation.
	a.rehydrateTodo(ctx, history)

	if a.cfg.Catalog != nil {
		err := a.cfg.Catalog.Prepare(workCtx)
		if err != nil {
			return history, fmt.Errorf("prepare tools: %w", err)
		}
	}

	return history, nil
}

type runState struct {
	observing                                              bool
	observationErr                                         error
	mutatingSucceeded, lastPromptTokens, lastCompactTokens int
	emptyFinishRetried, overflowRetried                    bool
	mutatedPaths                                           []string

	// repeatSignature is the previous step's call signature and repeatCount
	// is how many consecutive steps have now issued it. A model that stops
	// learning keeps asking the same question, so the loop stops handing it
	// tools after maxRepeatedSteps. Cleared on mutation: repeating a call
	// across a write is work, not a spin.
	repeatSignature    string
	repeatCount        int
	observations       [][32]byte
	observationWarning bool

	lastPromptEstimate int
	compactErr         error
}

func (st *runState) addPaths(paths []string) {
	for _, p := range paths {
		if !containsStr(st.mutatedPaths, p) {
			st.mutatedPaths = append(st.mutatedPaths, p)
		}
	}
}

// observeWorkspace measures effects during budget closing and finalization,
// including changes made by shell, MCP, or failed tool calls.
func (a *Agent) observeWorkspace(ctx context.Context, st *runState) {
	if !st.observing {
		return
	}

	paths, err := a.cfg.Changes.Observe(ctx)
	if err != nil {
		st.observationErr = err
		return
	}

	st.addPaths(paths)
	st.mutatingSucceeded = len(paths)
}
