package agent

import (
	"context"
	"fmt"
	"sync"
)

// DefaultMaxQuestions bounds ask_user rounds for a run.
const DefaultMaxQuestions = 3

// Asker is the ask_user budget for one run. It is shared across Run and
// Resume so a follow-up spends from the same allowance rather than getting
// a fresh one, which is how a chain of resumes turned a clarifying
// question into an unbounded conversation.
//
// It existed twice: the CLI kept the counter in a local and api.Session in
// a field, with the same limit written out in both.
type Asker struct {
	mu   sync.Mutex
	max  int
	used int
}

// NewAsker returns a budget of max questions. Zero takes
// DefaultMaxQuestions.
func NewAsker(max int) *Asker {
	if max <= 0 {
		max = DefaultMaxQuestions
	}
	return &Asker{max: max}
}

// Ask builds the ask_user implementation around a suspender.
//
// decorate may rewrite the prompt before it suspends, for callers that
// frame the pause with their own resume instructions; nil sends the
// question unchanged.
//
// Over the budget it returns an error telling the model to decide rather
// than asking again, since the operator has nothing left to give.
func (a *Asker) Ask(suspend Suspender, decorate func(question string) string) func(string) (string, error) {
	return func(question string) (string, error) {
		a.mu.Lock()
		a.used++
		used := a.used
		a.mu.Unlock()
		if used > a.max {
			return "", fmt.Errorf("ask_user: clarifying question limit (%d) reached — "+
				"make a decision and proceed with a stated assumption", a.max)
		}
		prompt := question
		if decorate != nil {
			prompt = decorate(question)
		}
		reply, err := suspend(context.Background(), SuspendRequest{Kind: SuspendAsk, Prompt: prompt})
		if err != nil {
			return "", err
		}
		return reply.Answer, nil
	}
}

// Used reports how many questions have been spent.
func (a *Asker) Used() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.used
}