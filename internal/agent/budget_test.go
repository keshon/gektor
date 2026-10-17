package agent

import (
	"context"
	"errors"
	"testing"
)

// A run that did not stop for MaxSteps must never be offered more budget.
func TestBudgetContinue_IgnoresOtherFailures(t *testing.T) {
	a := New(Config{Client: &stubClient{}, Tools: NewRegistry(), System: "sys"})
	asked := 0
	answer := func(context.Context, SuspendRequest) (SuspendReply, error) {
		asked++
		return SuspendReply{Answer: "y"}, nil
	}
	if _, _, ok := BudgetContinue(context.Background(), a, errors.New("boom"), "", answer); ok {
		t.Fatal("continued after an unrelated failure")
	}
	if asked != 0 {
		t.Fatalf("operator asked %d times, want 0", asked)
	}
}

// A denial falls through to the original error: the run fails exactly as
// it would have without the offer.
func TestBudgetContinue_DeclineLeavesTheRunFailing(t *testing.T) {
	a := New(Config{Client: &stubClient{}, Tools: NewRegistry(), System: "sys"})
	answer := func(context.Context, SuspendRequest) (SuspendReply, error) {
		return SuspendReply{Answer: "n"}, nil
	}
	_, _, ok := BudgetContinue(context.Background(), a, ErrMaxSteps, "state.json", answer)
	if ok {
		t.Fatal("a declined offer was treated as a continuation")
	}
}

// Headless callers pass a ClosedSuspender, and a run with nothing left
// open is not worth asking about.
func TestBudgetContinue_NeedsAnOperatorAndOpenWork(t *testing.T) {
	a := New(Config{Client: &stubClient{}, Tools: NewRegistry(), System: "sys"})
	// No open todos in the report, so the question is never worth asking
	// and the operator is never troubled.
	if _, _, ok := BudgetContinue(context.Background(), a, ErrMaxSteps, "state.json", ClosedSuspender("headless")); ok {
		t.Fatal("continued with no open work")
	}
}

