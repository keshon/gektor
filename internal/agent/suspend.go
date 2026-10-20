package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/keshon/gektor/internal/prompts"
)

// SuspendKind names what a run is waiting on. The loop itself never
// blocks on a human: tools and gates ask through a Suspender, and the
// implementation decides whether that means stdin (today), a channel
// (a future TUI or RPC server), or an error (headless runs).
type SuspendKind string

const (
	// SuspendAsk is a clarifying question from ask_user.
	SuspendAsk SuspendKind = "ask_user"
	// SuspendPermission is an Ask-gated tool call awaiting allow/deny.
	SuspendPermission SuspendKind = "permission"
	// SuspendBudget is an exhausted step budget with acknowledged work
	// unfinished, asking the operator for one more base budget. Deny,
	// error, or headless all decline — the run fails as before.
	SuspendBudget SuspendKind = "budget"
)

// SuspendRequest is one blocked gate.
type SuspendRequest struct {
	Kind SuspendKind
	// ID correlates the reply: the tool call ID for ask/permission, a
	// gate ID for plan approval.
	ID string
	// Prompt is what the operator must see and react to: the question,
	// the rendered plan, or the permission text.
	Prompt string
	// Tool and Resource further describe a permission gate.
	Tool     string
	Resource string
}

// SuspendReply answers one blocked gate. Only the fields matching the
// request kind matter: Answer for ask_user and plan notes, Allow/Always
// for permission and plan approval.
type SuspendReply struct {
	Answer string
	Allow  bool
	Always bool
}

// Suspender answers one blocked gate. Calling it may block as long as
// the transport needs — stdin, a channel, a socket — or fail immediately
// when nobody is there to answer.
type Suspender func(ctx context.Context, req SuspendRequest) (SuspendReply, error)

// Closed returns a Suspender that always fails: for runs with no
// operator, where blocking on stdin would hang until the wall clock
// kills the process. Fail-closed, never silent.
func ClosedSuspender(reason string) Suspender {
	return func(_ context.Context, req SuspendRequest) (SuspendReply, error) {
		return SuspendReply{}, fmt.Errorf("no operator to answer %s: %s", req.Kind, reason)
	}
}

// BudgetContinue offers one operator-approved continuation after the step
// limit. A saved transcript and an affirmative operator reply are required.
func BudgetContinue(ctx context.Context, a *Agent, runErr error, stateFile string, answer Suspender) (string, error, bool) {
	if !errors.Is(runErr, ErrMaxSteps) {
		return "", nil, false
	}
	if a == nil || stateFile == "" || answer == nil {
		return "", nil, false
	}
	open := a.Report().OpenTodos
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
		DefaultMaxSteps, remainingWork(open))
	previous, mutations := a.Report(), a.LastRunMutations
	maxSteps := a.cfg.MaxSteps
	a.cfg.MaxSteps = DefaultMaxSteps
	defer func() { a.cfg.MaxSteps = maxSteps }()
	ans, err := a.Resume(ctx, history, note)
	// Approval extends this run. Keep its measured effects, but never carry a
	// verification verdict across the new work without running it again.
	a.LastRunMutations += mutations
	a.report.Steps += previous.Steps
	a.report.MaxSteps += previous.MaxSteps
	paths := slices.Clone(previous.MutatedPaths)
	for _, path := range a.report.MutatedPaths {
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	a.report.MutatedPaths = paths
	for _, warning := range previous.Warnings {
		if !slices.Contains(a.report.Warnings, warning) {
			a.report.Warnings = append(a.report.Warnings, warning)
		}
	}
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

func remainingWork(open []string) string {
	if len(open) == 0 {
		return "continue the current task"
	}
	return shortOpen(open)
}
