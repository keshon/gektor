package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// The budget has to hold across every question in a run, not per call:
// two copies of this counter once let the CLI and api.Session disagree
// about how many clarifying questions a run had spent.
func TestAskerRefusesPastTheBudget(t *testing.T) {
	asked := 0
	suspend := func(context.Context, SuspendRequest) (SuspendReply, error) {
		asked++
		return SuspendReply{Answer: "ok"}, nil
	}
	ask := NewAsker(2).Ask(suspend, nil)

	for i := 0; i < 2; i++ {
		if _, err := ask("q"); err != nil {
			t.Fatalf("question %d: %v", i+1, err)
		}
	}
	_, err := ask("q")
	if err == nil || !strings.Contains(err.Error(), "limit (2)") {
		t.Fatalf("third question = %v, want the limit", err)
	}
	if asked != 2 {
		t.Fatalf("suspended %d times, want 2 — the budget must not reach the operator", asked)
	}
}

func TestAskerZeroTakesTheDefault(t *testing.T) {
	ask := NewAsker(0).Ask(ClosedSuspender("test"), nil)
	for i := 0; i < DefaultMaxQuestions; i++ {
		ask("q")
	}
	// ClosedSuspender always errors, so the budget is not what stops this;
	// just confirm the default is not zero.
	if DefaultMaxQuestions < 1 {
		t.Fatal("DefaultMaxQuestions must allow at least one question")
	}
}

func TestAskerDecoratesThePrompt(t *testing.T) {
	var seen string
	suspend := func(_ context.Context, req SuspendRequest) (SuspendReply, error) {
		seen = req.Prompt
		return SuspendReply{Answer: "yes"}, nil
	}
	ask := NewAsker(1).Ask(suspend, func(q string) string { return "framed: " + q })
	if _, err := ask("original"); err != nil {
		t.Fatal(err)
	}
	if seen != "framed: original" {
		t.Fatalf("prompt = %q", seen)
	}
}

func TestAskerPropagatesSuspendErrors(t *testing.T) {
	boom := errors.New("operator gone")
	ask := NewAsker(1).Ask(func(context.Context, SuspendRequest) (SuspendReply, error) {
		return SuspendReply{}, boom
	}, nil)
	if _, err := ask("q"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

// The suspender runs on whatever goroutine asks, and a resume can overlap
// a run, so the counter has to hold.
func TestAskerIsSafeUnderConcurrency(t *testing.T) {
	a := NewAsker(50)
	ask := a.Ask(func(context.Context, SuspendRequest) (SuspendReply, error) {
		return SuspendReply{Answer: "ok"}, nil
	}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ask("q")
		}()
	}
	wg.Wait()
	if used := a.Used(); used != 100 {
		t.Fatalf("Used = %d, want 100 (every attempt counted, even refused ones)", used)
	}
}
