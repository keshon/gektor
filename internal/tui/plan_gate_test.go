package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/events"
)

// planGateModel drives a model to an open plan gate and returns it with
// the suspender's reply channel.
func planGateModel(t *testing.T) (*model, chan agent.SuspendReply) {
	t.Helper()
	m := sizeModel(t, testModel())
	updated, _ := m.Update(eventMsg(events.Event{
		Name: "awaiting_input",
		Fields: map[string]any{
			"kind":   string(agent.SuspendPlan),
			"prompt": "=== proposed plan ===\n1. read the thing\n2. write the thing",
		},
	}))
	mm := updated.(*model)
	if mm.state != stAsk || !mm.planGate {
		t.Fatalf("state = %v planGate = %v, want stAsk with the plan gate", mm.state, mm.planGate)
	}
	answered := make(chan agent.SuspendReply, 1)
	go func() {
		rep, err := mm.hub.Suspender()(t.Context(), agent.SuspendRequest{Kind: agent.SuspendPlan})
		if err != nil {
			return
		}
		answered <- rep
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !mm.hub.HasPending() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return mm, answered
}

// A plan gate used to be an ordinary ask: the user had to type "y" into
// a text box, with no hint that y/n were meaningful.
func TestPlanGate_YApproves(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{{Code: 'y'}, {Code: 'Y'}} {
		mm, answered := planGateModel(t)
		updated, _ := mm.Update(key)
		got := updated.(*model)
		select {
		case rep := <-answered:
			if !strings.EqualFold(rep.Answer, "y") {
				t.Fatalf("reply = %+v, want yes", rep)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("plan gate never answered")
		}
		if got.state != stRunning {
			t.Errorf("state = %v, want stRunning", got.state)
		}
		if got.planGate {
			t.Error("planGate must clear once answered")
		}
	}
}

func TestPlanGate_NRejects(t *testing.T) {
	mm, answered := planGateModel(t)
	updated, _ := mm.Update(tea.KeyPressMsg{Code: 'n'})
	select {
	case rep := <-answered:
		if !strings.EqualFold(rep.Answer, "n") {
			t.Fatalf("reply = %+v, want no", rep)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("plan gate never answered")
	}
	if updated.(*model).state != stRunning {
		t.Errorf("state = %v, want stRunning", updated.(*model).state)
	}
}

// Anything else is a revision note, which is how a rejected plan gets
// its feedback. n alone must not swallow the first letter of it.
func TestPlanGate_TypingIsARevisionNote(t *testing.T) {
	mm, answered := planGateModel(t)
	var last *model = mm
	for _, r := range "split step 2" {
		updated, cmd := last.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		last = updated.(*model)
		_ = cmd
	}
	if last.state != stAsk {
		t.Fatalf("state = %v after typing, want the gate still open", last.state)
	}
	if got := last.input.Value(); got != "split step 2" {
		t.Errorf("input = %q, want the typed note", got)
	}
	if mm.hub.HasPending() == false {
		t.Error("typing must not answer the gate")
	}
	updated, _ := last.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case rep := <-answered:
		if rep.Answer != "split step 2" {
			t.Errorf("reply = %q, want the note", rep.Answer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("plan gate never answered")
	}
	_ = updated
}

// Y and N stay ordinary characters on a question that is not a plan.
func TestAskGate_KeepsYandNTypable(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.Update(eventMsg(events.Event{
		Name:   "awaiting_input",
		Fields: map[string]any{"kind": "ask_user", "prompt": "name the branch?"},
	}))
	mm := updated.(*model)
	if mm.planGate {
		t.Fatal("an ask_user must not be a plan gate")
	}
	var last *model = mm
	for _, r := range "yaml" {
		u, _ := last.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		last = u.(*model)
	}
	if got := last.input.Value(); got != "yaml" {
		t.Errorf("input = %q, want yaml typed through", got)
	}
}

func TestPlanGate_OverlaySaysWhatTheKeysDo(t *testing.T) {
	mm, _ := planGateModel(t)
	mm.refreshGate()
	body := mm.gateVP.View()
	for _, want := range []string{"Plan approval required", "Y approve", "N reject", "revision note", "write the thing"} {
		if !strings.Contains(body, want) {
			t.Errorf("gate body missing %q:\n%s", want, body)
		}
	}
	if got := mm.statusWord(); got != "awaiting plan approval" {
		t.Errorf("statusWord = %q", got)
	}
}
