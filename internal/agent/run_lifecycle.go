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
