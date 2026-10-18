package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/llm"
)

func TestParseNudgeLevel(t *testing.T) {
	for name, want := range map[string]NudgeLevel{
		"full":      NudgeFull,
		"FULL":      NudgeFull,
		" all ":     NudgeFull,
		"recovery":  NudgeRecovery,
		"essential": NudgeRecovery,
		"off":       NudgeOff,
		"none":      NudgeOff,
	} {
		got, ok := ParseNudgeLevel(name)
		if !ok || got != want {
			t.Errorf("ParseNudgeLevel(%q) = %v, %v; want %v, true", name, got, ok, want)
		}
	}
	// A typo must fail. Defaulting to full would make a mistyped
	// -nudges silently do nothing, which nobody would notice.
	if got, ok := ParseNudgeLevel("recover"); ok {
		t.Errorf("ParseNudgeLevel(\"recover\") = %v, true; want rejected", got)
	}
	for _, l := range []NudgeLevel{NudgeFull, NudgeRecovery, NudgeOff} {
		if _, ok := ParseNudgeLevel(NudgeLevelName(l)); !ok {
			t.Errorf("NudgeLevelName(%v) = %q does not round-trip", l, NudgeLevelName(l))
		}
	}
}

// Full is the zero level, so a caller that never sets one keeps the
// whole harness rather than losing it.
func TestNudgeLevel_ZeroIsFull(t *testing.T) {
	if NudgeLevel(0) != NudgeFull {
		t.Error("the zero NudgeLevel must be NudgeFull")
	}
	a := New(Config{})
	if !a.cfg.Nudges.allows(NudgeStuck) || !a.cfg.Nudges.allows(NudgeTodo) {
		t.Error("an unset config must allow every nudge")
	}
	if !a.nudges(NudgeTodo) {
		t.Error("nudges() must agree with allows() for an unset config")
	}
}

// Recovery drops the free advice and keeps the notices that buy another
// attempt; off drops both.
func TestNudgeLevels_SplitByCost(t *testing.T) {
	if !NudgeFull.allows(NudgeStuck) || !NudgeFull.allows(NudgeTodo) {
		t.Error("full must allow both kinds")
	}
	if NudgeRecovery.allows(NudgeStuck) {
		t.Error("recovery must drop the free advisory notices")
	}
	if NudgeRecovery.allows(NudgeBudget) {
		t.Error("recovery must drop the free budget notice")
	}
	if !NudgeRecovery.allows(NudgeTodo) {
		t.Error("recovery must keep the turn-costing notices")
	}
	for _, kind := range []string{NudgeStuck, NudgeBudget, NudgeTodo, NudgeVerify, NudgeTruncated} {
		if NudgeOff.allows(kind) {
			t.Errorf("off allowed %q, want dropped", kind)
		}
		// Recovery keeps the turn-costing notices and drops the free ones.
		if nudgeClasses[kind] == classAdvisory && NudgeRecovery.allows(kind) {
			t.Errorf("recovery allowed advisory %q, want dropped", kind)
		}
		if nudgeClasses[kind] == classRecovery && !NudgeRecovery.allows(kind) {
			t.Errorf("recovery dropped %q, which costs a turn", kind)
		}
	}
}

// A suppressed nudge drops the text but keeps the record: what the
// model was told and what happened are different facts.
func TestNudge_OffStillReports(t *testing.T) {
	var kinds, texts []string
	a := New(Config{
		Tools:  NewRegistry(),
		System: "sys",
		Nudges: NudgeOff,
		OnNudge: func(kind, text string) {
			kinds = append(kinds, kind)
			texts = append(texts, text)
		},
	})
	if history := a.nudge(nil, NudgeTodo, "todos open"); len(history) != 0 {
		t.Fatalf("off appended %d messages, want none", len(history))
	}
	if len(kinds) != 1 || kinds[0] != NudgeTodo {
		t.Fatalf("kinds = %v, want one todo report", kinds)
	}
	if !strings.HasPrefix(texts[0], harnessPrefix) {
		t.Errorf("reported text = %q, want the harness prefix", texts[0])
	}
}

func TestMarkNudge_SuppressedReturnsEmpty(t *testing.T) {
	a := New(Config{System: "sys", Nudges: NudgeRecovery})
	if got := a.markNudge(NudgeStuck, "same call three times"); got != "" {
		t.Errorf("markNudge(stuck) at recovery = %q, want empty", got)
	}
	if got := a.markNudge(NudgeTodo, "two todos open"); got == "" {
		t.Error("markNudge(todo) at recovery must still mark")
	}
}

// A suppressed bounce must not spend a turn saying nothing. Off accepts
// the truncation as an answer; full bounces and calls again.
func TestNudges_OffCostsNoTurn(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{FinishReason: "length", Message: llm.Message{Role: llm.RoleAssistant, Content: "cut off mid-"}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "answer"}},
	}}
	a := New(Config{
		Client:     client,
		Tools:      NewRegistry(),
		System:     "sys",
		SkipVerify: true,
		Nudges:     NudgeOff,
	})
	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.calls != 1 {
		t.Errorf("model called %d times, want 1: off must not bounce", client.calls)
	}
	if out != "cut off mid-" {
		t.Errorf("answer = %q, want the stump accepted as-is", out)
	}
}

func TestNudges_FullBounces(t *testing.T) {
	client := &stubClient{responses: []llm.ChatResponse{
		{FinishReason: "length", Message: llm.Message{Role: llm.RoleAssistant, Content: "cut off mid-"}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "answer"}},
	}}
	var kinds []string
	a := New(Config{
		Client:     client,
		Tools:      NewRegistry(),
		System:     "sys",
		SkipVerify: true,
		Nudges:     NudgeFull,
		OnNudge:    func(kind, _ string) { kinds = append(kinds, kind) },
	})
	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.calls != 2 {
		t.Errorf("model called %d times, want 2: full must bounce", client.calls)
	}
	if out != "answer" {
		t.Errorf("answer = %q, want the second turn's", out)
	}
	if len(kinds) != 1 || kinds[0] != NudgeTruncated {
		t.Errorf("kinds = %v, want one truncated report", kinds)
	}
	heard := false
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, harnessPrefix) {
			heard = true
		}
	}
	if !heard {
		t.Error("full must put the nudge in history")
	}
}

// Recovery is the middle: it keeps the bounce and its text, and only
// the free advice is dropped. Asserted through interject, which is the
// only source of advisory nudges.
func TestNudges_RecoveryDropsInterjectAdvice(t *testing.T) {
	for _, tc := range []struct {
		level     NudgeLevel
		wantWords bool
	}{
		{NudgeFull, true},
		{NudgeRecovery, false},
		{NudgeOff, false},
	} {
		a := New(Config{Tools: NewRegistry(), System: "sys", Nudges: tc.level})
		var st runState
		st.stuckSteps = a.cfg.MaxStuckSteps // always tripped
		got := a.interject(&st, 3, stepOutcome{})
		if tc.wantWords && got == "" {
			t.Errorf("%v: interject returned nothing, want the stuck escalation", tc.level)
		}
		if !tc.wantWords && got != "" {
			t.Errorf("%v: interject returned %q, want nothing", tc.level, got)
		}
	}
}