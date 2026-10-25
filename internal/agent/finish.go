package agent

import (
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/prompts"
)

type finishOutcome struct {
	done    bool
	answer  string
	err     error
	history []llm.Message
}

// Only incomplete or malformed responses need another model turn.
func (a *Agent) handleFinish(st *runState, resp llm.ChatResponse, leaked bool, history []llm.Message) finishOutcome {
	if resp.FinishReason == "length" {
		return finishOutcome{history: a.nudge(history, NudgeTruncated, prompts.Truncated+a.hint("truncated"))}
	}
	if leaked {
		return finishOutcome{history: a.nudge(history, NudgeLeak, prompts.LeakDetected)}
	}
	if strings.TrimSpace(resp.Message.Content) == "" {
		if !st.emptyFinishRetried {
			st.emptyFinishRetried = true
			return finishOutcome{history: a.nudge(history, NudgeTruncated, "The response was empty. Return an answer or a tool call.")}
		}
		return finishOutcome{done: true, err: fmt.Errorf("empty response after retry")}
	}
	a.report.Final = resp.Message.Content
	return finishOutcome{done: true, answer: resp.Message.Content, history: history}
}

const (
	NudgeOverflow     = "overflow"
	NudgeTruncated    = "truncated"
	NudgeLeak         = "leak"
	NudgeCompact      = "compact"
	NudgeRepeating    = "repeating"
	NudgeVerification = "verification"
	NudgeRecovery     = "capability_recovery"
	harnessPrefix     = "[harness] "
)

func harnessText(text string) string { return harnessPrefix + text }

func (a *Agent) notifyNudge(kind, text string) {
	if a.cfg.OnNudge != nil {
		a.cfg.OnNudge(kind, text)
	}
}

func (a *Agent) nudge(history []llm.Message, kind, text string) []llm.Message {
	marked := harnessText(text)
	a.notifyNudge(kind, marked)
	return append(history, llm.Message{Role: llm.RoleUser, Content: marked, Harness: kind})
}
