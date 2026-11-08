package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/session"
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
	if pending := a.cfg.Inbox.stop(); len(pending) > 0 {
		history = a.acceptInputs(a.snapshot.Context(), pending, st)
	}
	if ctxErr := ctx.Err(); ctxErr != nil && (runErr == nil || errors.Is(runErr, ErrMaxSteps)) {
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
				history = a.nudge(history, NudgeVerification, fmt.Sprintf("Verification failed: %v\n%s", err, a.report.Verification))
				a.saveState(history)
			}
		}
	}
	a.observeWorkspace(st)
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
		a.snapshot.Runs = append(a.snapshot.Runs, session.Run{Start: runStart, End: len(a.snapshot.History), Outcome: a.report.Outcome, Steps: a.report.Steps, Verification: a.report.Verification, Error: session.FailureText(runErr)})
		if err := a.snapshot.Save(a.cfg.StateFile); err != nil {
			a.report.Warnings = append(a.report.Warnings, "run boundary persistence failed: "+err.Error())
		}
	}
	return answer, runErr
}

// restoreRun validates wiring and restores saved selections before opening a run.
func (a *Agent) restoreRun(ctx context.Context) error {
	if err := a.cfg.Tools.Err(); err != nil {
		return fmt.Errorf("tool registry: %w", err)
	}
	if err := a.cfg.Policy.Validate(); err != nil {
		return err
	}
	if a.cfg.RestoreBrowser != nil {
		if err := a.cfg.RestoreBrowser(a.snapshot.Browser); err != nil {
			return fmt.Errorf("restore browser profile: %w", err)
		}
	}
	if a.cfg.RestoreCapabilities != nil {
		if err := a.cfg.RestoreCapabilities(ctx, a.snapshot.Capabilities); err != nil {
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
	if n := len(a.snapshot.History); hasRequest && n > 0 && len(a.snapshot.History[n-1].ToolCalls) > 0 {
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
	if a.cfg.BeginChanges != nil {
		var err error
		st.observeChanges, err = a.cfg.BeginChanges(workCtx)
		if err != nil {
			return history, fmt.Errorf("observe workspace: %w", err)
		}
	}
	// Restore the task checklist when resuming a saved conversation.
	a.rehydrateTodo(ctx, history)
	if a.cfg.RestoreTools != nil {
		if err := a.cfg.RestoreTools(workCtx, a.snapshot.History); err != nil {
			if ctx.Err() != nil {
				return history, ctx.Err()
			}
			history = a.nudge(history, NudgeRecovery, "Optional session tools unavailable: "+err.Error()+". Continue with available tools or explicitly retry activation.")
		}
	}

	if a.cfg.PrepareTools != nil {
		if err := a.cfg.PrepareTools(workCtx); err != nil {
			return history, fmt.Errorf("prepare tools: %w", err)
		}
	}
	return history, nil
}
