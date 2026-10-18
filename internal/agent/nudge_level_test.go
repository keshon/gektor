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
		if nudgeAllowed(NudgeOff, kind) {
			t.Errorf("NudgeOff still delivers %q", kind)
		}
		if !nudgeAllowed(NudgeFull, kind) {
			t.Errorf("NudgeFull drops %q", kind)
		}
	}
}

// The compaction notice is not advice. Turning it off leaves the model
// reasoning over a window it believes is complete, so it is never a choice -
// but it must not be quietly absent either.
func TestCompactionNoticeIsNotSuppressible(t *testing.T) {
	for _, level := range []NudgeLevel{NudgeFull, NudgeRecovery, NudgeOff} {
		if !nudgeAllowed(level, NudgeCompact) {
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
		Nudges:  NudgeOff,
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

// everyNudgeKind is the full set. A kind missing from nudgeClasses falls
// through allows as a recovery nudge, which is exactly the silent default
// that made the previous advisory set wrong.
func everyNudgeKind() []string {
	return []string{
		NudgeOverflow, NudgeTruncated, NudgeThinkWrap, NudgeLeak, NudgeVerify,
		NudgeRefusal, NudgeBudget, NudgeStuck, NudgeTodo, NudgeClosing,
		NudgeWrapUp, NudgeCompact,
	}
}

func TestEveryNudgeKindHasAClass(t *testing.T) {
	for _, kind := range everyNudgeKind() {
		if _, ok := nudgeClasses[kind]; !ok {
			t.Errorf("%q has no class; it would default to recovery", kind)
		}
	}
	// And nothing classified that the loop cannot produce, which would be a
	// class nobody ever reads.
	for kind := range nudgeClasses {
		found := false
		for _, k := range everyNudgeKind() {
			if k == kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q is classified but never produced", kind)
		}
	}
}

// The three levels are presets over three classes. Each has to be exactly
// the set of kinds its name promises, or the names are decoration.
func TestLevelsArePresetsOverClasses(t *testing.T) {
	want := map[NudgeLevel][]nudgeClass{
		NudgeFull:     {classFact, classRecovery, classAdvisory},
		NudgeRecovery: {classFact, classRecovery},
		NudgeOff:      {classFact},
	}
	for level, classes := range want {
		for _, kind := range everyNudgeKind() {
			allowed := nudgeAllowed(level, kind)
			expected := false
			for _, c := range classes {
				if nudgeClasses[kind] == c {
					expected = true
				}
			}
			if allowed != expected {
				t.Errorf("%v allows %q = %v, want %v", NudgeLevelName(level), kind, allowed, expected)
			}
		}
	}
}
