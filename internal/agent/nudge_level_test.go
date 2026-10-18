package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/llm"
)

// "off" has to mean off. It did not: two kinds of harness text reached the
// model anyway, and one of them reached it without saying who wrote it.

// Every kind the loop can put in front of the model is either advice the
// user can refuse, or a fact about the conversation they cannot.
func TestOffSuppressesEveryKindOfAdvice(t *testing.T) {
	advice := []string{
		NudgeOverflow, NudgeTruncated, NudgeThinkWrap, NudgeLeak, NudgeVerify,
		NudgeRefusal, NudgeBudget, NudgeStuck, NudgeTodo, NudgeClosing,
		NudgeWrapUp,
	}
	for _, kind := range advice {
		if NudgeOff.allows(kind) {
			t.Errorf("NudgeOff still delivers %q", kind)
		}
		if !NudgeFull.allows(kind) {
			t.Errorf("NudgeFull drops %q", kind)
		}
	}
}

// The compaction notice is not advice. Turning it off leaves the model
// reasoning over a window it believes is complete, so it is never a choice -
// but it must not be quietly absent either.
func TestCompactionNoticeIsNotSuppressible(t *testing.T) {
	for _, level := range []NudgeLevel{NudgeFull, NudgeRecovery, NudgeOff} {
		if !level.allows(NudgeCompact) {
			t.Errorf("%v would drop the compaction notice", NudgeLevelName(level))
		}
	}
}

// A suppression still has to reach observers: turning the harness down is a
// change to what the model is told, not an erasure of the record.
func TestSuppressedAdviceIsStillReported(t *testing.T) {
	var seen []string
	a := New(Config{
		Client: &stubClient{}, Tools: NewRegistry(), System: "sys", SkipVerify: true,
		Nudges: NudgeOff,
		OnNudge: func(kind, _ string) { seen = append(seen, kind) },
	})
	history := a.nudge(nil, NudgeVerify, "verify failed")
	if len(history) != 0 {
		t.Fatalf("NudgeOff put %d messages in history", len(history))
	}
	if len(seen) != 1 || seen[0] != NudgeVerify {
		t.Fatalf("observer heard %v, want one verify report", seen)
	}
}

// The closing instruction used to go in as a bare user turn, so the model
// read a loop instruction as something the person had typed.
func TestWrapUpIsAttributed(t *testing.T) {
	var seen []string
	a := New(Config{
		Client: &stubClient{responses: []llm.ChatResponse{
			{Message: llm.Message{Role: llm.RoleAssistant, Content: "summary"}},
		}},
		Tools: NewRegistry(), System: "sys", SkipVerify: true,
		Nudges:   NudgeFull,
		MaxSteps: 1,
		OnNudge:  func(kind, text string) { seen = append(seen, kind+":"+text) },
	})
	var st runState
	a.report.Steps = 1
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "task"},
	}
	a.wrapUpTurn(context.Background(), &st, history)
	var attributed bool
	for _, s := range seen {
		if strings.HasPrefix(s, NudgeWrapUp+":") && strings.Contains(s, harnessPrefix) {
			attributed = true
		}
	}
	if !attributed {
		t.Fatalf("the wrap-up was not reported as attributed harness text: %v", seen)
	}
}

// The advertised shape of the enum has to match what it does, or the command
// summary is a lie in the same way the old off mode was.
func TestEveryNudgeLevelRoundTrips(t *testing.T) {
	for _, name := range []string{"full", "recovery", "off"} {
		level, ok := ParseNudgeLevel(name)
		if !ok {
			t.Fatalf("ParseNudgeLevel(%q) failed", name)
		}
		if got := NudgeLevelName(level); got != name {
			t.Errorf("%q parsed then rendered as %q", name, got)
		}
	}
}
