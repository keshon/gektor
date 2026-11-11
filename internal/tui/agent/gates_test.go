package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/permissionlog"
	"github.com/keshon/gektor/internal/workspace"
)

func TestPermissionKeysAnswer(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name:   "awaiting_input",
		Fields: map[string]any{"kind": "permission", "prompt": "allow?"},
	}))
	mm := updated.(*Model)
	if mm.state != stPermission {
		t.Fatalf("state = %v", mm.state)
	}
	// Suspend in the background: key handling must wake it.
	answered := make(chan agent.SuspendReply, 1)
	go func() {
		rep, err := mm.hub.Suspender()(t.Context(), agent.SuspendRequest{Kind: agent.SuspendPermission})
		if err != nil {
			return
		}
		answered <- rep
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !mm.hub.HasPending() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Text: string([]rune{'y'})})
	mm = updated.(*Model)
	if mm.state != stRunning {
		t.Fatalf("state = %v after y", mm.state)
	}
	select {
	case rep := <-answered:
		if rep.Answer != "y" {
			t.Fatalf("reply = %+v", rep)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("suspender never woke")
	}
}

func TestAskEnterSubmits(t *testing.T) {
	m := sizeModel(t, testModel())
	updated, _ := m.testUpdate(eventMsg(events.Event{
		Name:   "awaiting_input",
		Fields: map[string]any{"kind": "ask_user", "prompt": "which?"},
	}))
	mm := updated.(*Model)
	if mm.state != stAsk {
		t.Fatalf("state = %v", mm.state)
	}
	answered := make(chan agent.SuspendReply, 1)
	go func() {
		rep, err := mm.hub.Suspender()(t.Context(), agent.SuspendRequest{Kind: agent.SuspendAsk})
		if err != nil {
			return
		}
		answered <- rep
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !mm.hub.HasPending() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	mm.input.SetValue("assumed")
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = updated.(*Model)
	select {
	case rep := <-answered:
		if rep.Answer != "assumed" {
			t.Fatalf("reply = %+v", rep)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("suspender never woke")
	}
	if mm.state != stRunning {
		t.Fatalf("state = %v after enter", mm.state)
	}
}

func TestAskCtrlQStopsBeforeQuit(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stAsk
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if mm := updated.(*Model); mm.PrepareQuit() || mm.state != stStopping {
		t.Fatal("ctrl+q must stop the active ask before quitting")
	}
}

func TestScrollRoutedInEveryState(t *testing.T) {
	for _, state := range []runState{stRunning, stDone} {
		m := sizeModel(t, testModel())
		for i := 0; i < 40; i++ {
			m.appendBlock(markerBlock("block"))
		}
		if !m.transcript.Follow {
			t.Fatalf("state %v: follow expected while at bottom", state)
		}
		m.state = state
		updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyPgUp})
		if mm := updated.(*Model); mm.transcript.Follow {
			t.Fatalf("state %v: pgup must unfollow", state)
		}
	}
	// Wheel scrolls outside the running state too.
	m := sizeModel(t, testModel())
	for i := 0; i < 40; i++ {
		m.appendBlock(markerBlock("block"))
	}
	m.state = stDone
	wheel := tea.MouseWheelMsg{Button: tea.MouseWheelUp}
	updated, _ := m.testUpdate(wheel)
	if mm := updated.(*Model); mm.transcript.Follow {
		t.Fatal("wheel-up in done state must unfollow")
	}
}

func TestGateStages_AlwaysConfirm(t *testing.T) {
	mm, answered := openPermissionGate(t)
	// "a" escalates to confirm, answering nothing yet.
	updated, _ := mm.testUpdate(keyRunes('a'))
	mm = updated.(*Model)
	if mm.gstage != gsAlways || mm.state != stPermission {
		t.Fatalf("stage = %v state = %v, want confirm", mm.gstage, mm.state)
	}
	if !mm.hub.HasPending() {
		t.Fatal("confirm stage must not answer yet")
	}
	if got := mm.gateBar(); !strings.Contains(got, "run_shell") {
		t.Fatalf("confirm must name the gated call: %q", got)
	}
	// Esc backs out, still pending.
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Code: tea.KeyEsc})
	mm = updated.(*Model)
	if mm.gstage != gsPermit || !mm.hub.HasPending() {
		t.Fatal("esc must return to permit, still pending")
	}
	// Enter on confirm answers "a" and tears the overlay down.
	updated, _ = mm.testUpdate(keyRunes('a'))
	mm = updated.(*Model)
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = updated.(*Model)
	if rep := awaitReply(t, answered); rep.Answer != "a" {
		t.Fatalf("reply = %+v", rep)
	}
	if mm.state != stRunning || len(mm.overlays) != 0 {
		t.Fatalf("state = %v overlays = %d, want teardown", mm.state, len(mm.overlays))
	}
}

func TestGateStages_RejectNote(t *testing.T) {
	mm, answered := openPermissionGate(t)
	// "n" drops to the note stage, answering nothing yet.
	updated, _ := mm.testUpdate(keyRunes('n'))
	mm = updated.(*Model)
	if mm.gstage != gsReject || !mm.hub.HasPending() {
		t.Fatal("n must open the note stage, still pending")
	}
	if !mm.note.Focused() {
		t.Fatal("note must take focus")
	}
	for _, r := range "use cat" {
		updated, _ = mm.testUpdate(keyRunes(r))
		mm = updated.(*Model)
	}
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = updated.(*Model)
	if rep := awaitReply(t, answered); rep.Answer != "n: use cat" {
		t.Fatalf("reply = %+v", rep)
	}
	if mm.state != stRunning || len(mm.overlays) != 0 {
		t.Fatalf("state = %v overlays = %d, want teardown", mm.state, len(mm.overlays))
	}
	found := false
	for _, b := range mm.transcript.blocks {
		if b.role == roleUser && strings.Contains(b.text, "use cat") {
			found = true
		}
	}
	if !found {
		t.Fatalf("note must echo as user block: %+v", mm.transcript.blocks)
	}
}

func TestGateRejectEmptyNoteDenies(t *testing.T) {
	mm, answered := openPermissionGate(t)
	updated, _ := mm.testUpdate(keyRunes('n'))
	mm = updated.(*Model)
	updated, _ = mm.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = updated.(*Model)
	if len(mm.overlays) != 0 {
		t.Fatal("deny must close the permission gate")
	}
	if rep := awaitReply(t, answered); rep.Answer != "n" {
		t.Fatalf("reply = %+v, want plain deny", rep)
	}
}

func TestGateHook_AuditsDecisions(t *testing.T) {
	m := sizeModel(t, testModel())
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	hook := permissionlog.WrapGate(path, "tui", m.contextGate(m.hub, ws))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = hook(context.Background(), "read_file", "a.txt", json.RawMessage(`{}`))
	}()
	waitPending(t, m.hub)
	if err := m.hub.Respond("y"); err != nil {
		t.Fatal(err)
	}
	<-done
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"front":"tui"`) || !strings.Contains(string(data), `"effect":"allow"`) {
		t.Fatalf("audit record missing: %s", data)
	}
}

func TestContextGate_AlwaysMemory(t *testing.T) {
	m := sizeModel(t, testModel())
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	gate := m.contextGate(m.hub, ws)
	hook := func(tool, resource string, args json.RawMessage) (permission.Effect, error) {
		return gate(context.Background(), tool, resource, args)
	}
	type result struct {
		eff permission.Effect
		err error
	}
	first := make(chan result, 1)
	go func() {
		eff, err := hook("read_file", "notes.txt", json.RawMessage(`{}`))
		first <- result{eff, err}
	}()
	waitPending(t, m.hub)
	if err := m.hub.Respond("a"); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-first:
		if r.eff != permission.Allow || r.err != nil {
			t.Fatalf("first = %v, %v", r.eff, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hook never returned")
	}
	// Repeat: auto-allowed with no suspension.
	if eff, err := hook("read_file", "notes.txt", json.RawMessage(`{}`)); eff != permission.Allow || err != nil {
		t.Fatalf("repeat = %v, %v", eff, err)
	}
	if m.hub.HasPending() {
		t.Fatal("repeat must not suspend")
	}
	// "y" allows once: the same pair suspends again next time.
	second := make(chan result, 1)
	go func() {
		eff, err := hook("read_file", "other.txt", json.RawMessage(`{}`))
		second <- result{eff, err}
	}()
	waitPending(t, m.hub)
	if err := m.hub.Respond("y"); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-second:
		if r.eff != permission.Allow || r.err != nil {
			t.Fatalf("second = %v, %v", r.eff, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hook never returned")
	}
	third := make(chan result, 1)
	go func() {
		eff, err := hook("read_file", "other.txt", json.RawMessage(`{}`))
		third <- result{eff, err}
	}()
	waitPending(t, m.hub)
	if err := m.hub.Respond("y"); err != nil {
		t.Fatal(err)
	}
	<-third
}

func TestOverlayPushPopRestore(t *testing.T) {
	m := sizeModel(t, testModel())
	if !m.input.Focused() {
		t.Fatal("test setup: input must start focused")
	}
	m.pushOverlay()
	m.applyFocus(focusNote)
	if m.input.Focused() || !m.note.Focused() {
		t.Fatal("applyFocus(note) must move focus")
	}
	m.popOverlay()
	if !m.input.Focused() || m.note.Focused() {
		t.Fatal("pop must restore input focus")
	}
	if len(m.overlays) != 0 {
		t.Fatal("stack must drain")
	}
	m.popOverlay() // unbalanced pop: no-op, never a panic
}

func TestGateEventForwardsFields(t *testing.T) {
	ev := gateEvent("awaiting_input", map[string]any{
		"kind": "permission", "prompt": "allow?",
		"tool": "run_shell", "resource": "go test",
	})
	if ev.Fields["tool"] != "run_shell" || ev.Fields["resource"] != "go test" || ev.Fields["prompt"] != "allow?" {
		t.Fatalf("fields = %v", ev.Fields)
	}
}
