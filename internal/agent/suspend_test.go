package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestClosedSuspender_Fails(t *testing.T) {
	s := ClosedSuspender("unattended test run")
	_, err := s(context.Background(), SuspendRequest{Kind: SuspendAsk, Prompt: "q?"})
	if err == nil || !strings.Contains(err.Error(), "ask_user") {
		t.Fatalf("want kind-naming error, got %v", err)
	}
}

func TestSuspendKinds_Distinct(t *testing.T) {
	if SuspendAsk == SuspendPermission ||
		SuspendBudget == SuspendAsk || SuspendBudget == SuspendPermission {
		t.Fatal("suspend kinds must be distinct")
	}
}

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

func TestBudgetContinuePreservesEffectsAndGrantsExactlyTheOfferedBudget(t *testing.T) {
	for _, base := range []int{1, 40} {
		t.Run(fmt.Sprint(base), func(t *testing.T) {
			state := filepath.Join(t.TempDir(), "state.json")
			client := &stubClient{responses: []llm.ChatResponse{say("done")}}
			a := New(Config{Client: client, Tools: NewRegistry(), MaxSteps: base, StateFile: state})
			a.report = RunReport{Steps: base, MaxSteps: base, MutatedPaths: []string{"a.txt"}, OpenTodos: []string{"work"}, Verification: "PASSED old", Warnings: []string{"old warning"}}
			a.LastRunMutations = 1
			if err := SaveState(state, []llm.Message{{Role: llm.RoleSystem, Content: ""}, {Role: llm.RoleUser, Content: "task"}}); err != nil {
				t.Fatal(err)
			}
			observedBudget := 0
			a.cfg.OnStep = func(_ int, _ llm.Message) { observedBudget = a.Report().MaxSteps }
			ans, err, ok := BudgetContinue(t.Context(), a, ErrMaxSteps, state, func(_ context.Context, req SuspendRequest) (SuspendReply, error) {
				if !strings.Contains(req.Prompt, "25") {
					t.Fatalf("wrong offer: %s", req.Prompt)
				}
				return SuspendReply{Answer: "yes"}, nil
			})
			r := a.Report()
			if err != nil || !ok || ans != "done" {
				t.Fatalf("result=%q err=%v ok=%v", ans, err, ok)
			}
			if observedBudget != DefaultMaxSteps || a.cfg.MaxSteps != base || r.MaxSteps != base+DefaultMaxSteps || r.Steps != base+1 {
				t.Fatalf("budgets wrong: observed=%d cfg=%+v report=%+v", observedBudget, a.cfg, r)
			}
			if len(r.MutatedPaths) != 1 || r.MutatedPaths[0] != "a.txt" || a.LastRunMutations != 1 || len(r.Warnings) != 1 || r.Verification != "" {
				t.Fatalf("effects/verdict wrong: %+v", r)
			}
		})
	}
}

func TestBudgetContinueAccumulatesRealRunsWithOverlappingPaths(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	client := &stubClient{responses: []llm.ChatResponse{writeStep("1", "a.txt"), writeStep("2", "b.txt"), writeStep("3", "a.txt"), say("done")}}
	a := New(Config{Client: client, Tools: NewRegistry(mutStub{"write_file"}, todoFake{open: []string{"work"}}), MaxSteps: 1, StateFile: state})
	_, err := a.Run(t.Context(), "write both files")
	if !errors.Is(err, ErrMaxSteps) || len(a.MutatedPaths()) != 1 {
		t.Fatalf("initial run: %+v %v", a.Report(), err)
	}
	ans, err, ok := BudgetContinue(t.Context(), a, err, state, func(context.Context, SuspendRequest) (SuspendReply, error) { return SuspendReply{Answer: "yes"}, nil })
	r := a.Report()
	if err != nil || !ok || ans != "done" || !slices.Equal(r.MutatedPaths, []string{"a.txt", "b.txt"}) || a.LastRunMutations != 3 || r.Steps != 4 || r.MaxSteps != 26 {
		t.Fatalf("continuation: %+v mutations=%d ans=%q err=%v", r, a.LastRunMutations, ans, err)
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
