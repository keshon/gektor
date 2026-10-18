package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/keshon/tars/internal/prompts"
)

// BudgetContinue asks the operator for one more base budget when a run
// dies at MaxSteps with acknowledged work unfinished and a state file to
// resume from.
//
// One continuation per call, not a loop: piping yes into stdin must not buy
// unbounded runs. A denial, an answer error, no operator (ClosedSuspender),
// a missing state file and no open todos all fall through to the original
// error, so the run fails exactly as it would have.
//
// This lives here rather than in a transport because it is about the run,
// not about how it was started: the API session and the CLI both owe their
// operator the same offer, and one of them had been silently missing it.
func BudgetContinue(ctx context.Context, a *Agent, runErr error, stateFile string, answer Suspender) (string, error, bool) {
	if !errors.Is(runErr, ErrMaxSteps) {
		return "", nil, false
	}
	if a == nil || stateFile == "" || answer == nil {
		return "", nil, false
	}
	open := a.Report().OpenTodos
	if len(open) == 0 {
		return "", nil, false
	}
	rep, err := answer(ctx, SuspendRequest{
		Kind: SuspendBudget, ID: "budget",
		Prompt: fmt.Sprintf(prompts.BudgetContinue,
			a.Report().MaxSteps, len(open), shortOpen(open), DefaultMaxSteps),
	})
	if err != nil {
		return "", nil, false
	}
	if ans := strings.ToLower(strings.TrimSpace(rep.Answer)); ans != "y" && ans != "yes" {
		return "", nil, false
	}
	history, err := LoadState(stateFile)
	if err != nil {
		return "", nil, false
	}
	// The [harness] prefix matches harnessText, which is unexported: this
	// note is generated, not operator-typed.
	note := fmt.Sprintf("[harness] Step budget extended by operator approval (+%d steps). Continue the remaining work: %s. Do not restart finished items.",
		DefaultMaxSteps, shortOpen(open))
	ans, err := a.Resume(ctx, history, note)
	return ans, err, true
}

// shortOpen renders open todos for operator and model consumption, capped:
// the list itself can run to fifty items.
func shortOpen(open []string) string {
	const max = 5
	if len(open) > max {
		return strings.Join(open[:max], ", ") +
			fmt.Sprintf(" (+%d more)", len(open)-max)
	}
	return strings.Join(open, ", ")
}
