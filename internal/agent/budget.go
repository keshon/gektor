package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

var ErrRunBudget = errors.New("run time budget exhausted; task incomplete")

func runBudget(ctx context.Context, limit time.Duration) (context.Context, context.Context, context.CancelFunc) {
	if limit <= 0 {
		return ctx, ctx, func() {}
	}
	hard, cancelHard := context.WithTimeout(ctx, limit)
	deadline, _ := hard.Deadline()
	reserve := min(30*time.Second, time.Until(deadline)/5)
	work, cancelWork := context.WithDeadline(hard, deadline.Add(-reserve))
	return hard, work, func() { cancelWork(); cancelHard() }
}

func (a *Agent) closeBudget(ctx context.Context, st *runState, history []llm.Message) {
	a.observeWorkspace(st)
	_, open := a.cfg.Tools.TodoProgress()
	a.report.Summary = fmt.Sprintf("Time budget exhausted; task incomplete. Observed changed paths: %v. Open tasks: %v. Resume to continue; completion has not been verified.", st.mutatedPaths, open)
	a.report.Warnings = append(a.report.Warnings, ErrRunBudget.Error())
	// Closing uses the original hard deadline and step allowance. Cancellation
	// never starts another request, and a failed closing request keeps the facts above.
	if ctx.Err() != nil || a.report.Steps >= a.cfg.MaxSteps {
		return
	}
	history = a.nudge(history, "time_limit", "Work time has ended. Tools are disabled. Briefly report completed work, verification actually observed, blockers and remaining work. The task is incomplete; do not claim success.")
	a.saveState(history)
	resp, err := a.chat(ctx, llm.ChatRequest{Messages: a.reuseContext(history), MaxTokens: min(1024, a.effectiveMaxTokens(st.lastPromptTokens))})
	a.report.Steps++
	if err != nil || resp.FinishReason == "length" || len(resp.Message.ToolCalls) > 0 || strings.TrimSpace(resp.Message.Content) == "" {
		return
	}
	_, cleaned, leaked := llm.ExtractLeakedCalls(resp.Message.Content, a.knownTools())
	if leaked {
		return
	}
	resp.Message.Content = cleaned
	a.report.Summary = "Task incomplete: " + cleaned
	history = append(history, recordableMessage(resp.Message))
	a.saveState(history)
	if a.cfg.OnStep != nil {
		a.cfg.OnStep(a.report.Steps-1, resp.Message)
	}
	if a.cfg.OnUsage != nil {
		a.cfg.OnUsage(a.report.Steps-1, resp.Usage)
	}
}
