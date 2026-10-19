package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/events"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/roles"
	"github.com/keshon/tars/internal/run"
	"github.com/keshon/tars/internal/tools"
	"github.com/keshon/tars/internal/workspace"
)

// liveStreamClient streams if asked and answers unary otherwise, so a
// session's choice of path is observable.
type liveStreamClient struct{ streamed int }

func (c *liveStreamClient) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{
		Message:      llm.Message{Role: llm.RoleAssistant, Content: "one two three"},
		FinishReason: "stop",
	}, nil
}

func (c *liveStreamClient) Stream(_ context.Context, _ llm.ChatRequest, _, onContent func(string)) (llm.ChatResponse, error) {
	c.streamed++
	for _, chunk := range []string{"one ", "two ", "three"} {
		onContent(chunk)
	}
	return llm.ChatResponse{
		Message:      llm.Message{Role: llm.RoleAssistant, Content: "one two three"},
		FinishReason: "stop",
	}, nil
}

// The two halves of live streaming were each tested alone: that the model
// reveals delta events it is handed, and that a session emits them. What
// was never tested is the join — that a session configured the way the TUI
// configures it, with OnDelta already stubbed to a no-op by
// withoutPrintHooks, still reaches the model's live buffer.
//
// A silent break here shows as a finished reply with nothing before it,
// which is exactly what a lost reveal looks like.
func TestSessionDeltasReachTheLiveBuffer(t *testing.T) {
	dir := t.TempDir()
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	m := sizeModel(t, testModel())
	m.state = stRunning

	// The stub the TUI installs before handing the env to a session.
	stubbed := withoutPrintHooks(roles.Env{})
	if stubbed.OnDelta == nil {
		t.Fatal("withoutPrintHooks must leave a callable OnDelta behind")
	}

	client := &liveStreamClient{}
	seen := map[string]int{}
	// The step event resets the live buffer, and the reveal only happens
	// on a frame tick, so what matters is that each delta is queued as it
	// arrives rather than that anything survives to the end of the run.
	var queuedAfterDelta []string
	sess, err := run.New(run.Config{
		Task: "task",
		Env: roles.Env{
			Client: client,
			WS:     ws,
			Procs:  tools.NewBackgroundProcesses(),
			Stream: true,
			// Exactly what the TUI passes: print hooks already stubbed.
			OnDelta: stubbed.OnDelta,
		},
		OnEvent: func(ev events.Event) {
			seen[ev.Name]++
			// bubbletea Update is value semantics: the returned model is
			// the one carrying the state, exactly as the real loop keeps it.
			updated, _ := m.Update(eventMsg(ev))
			if next, ok := updated.(*model); ok {
				m = next
			}
			if ev.Name == "delta" {
				queuedAfterDelta = append(queuedAfterDelta, m.liveQueue)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if client.streamed == 0 {
		t.Fatal("the session took the unary path, so nothing could stream")
	}
	if seen["delta"] == 0 {
		t.Fatalf("no delta events reached the model: %v", seen)
	}
	// Each delta must be visible as queued text, growing, rather than
	// arriving already collapsed into the final answer.
	if len(queuedAfterDelta) < 2 {
		t.Fatalf("expected several deltas, got %v", queuedAfterDelta)
	}
	var joined string
	for i, q := range queuedAfterDelta {
		if q == "" {
			t.Fatalf("delta %d queued nothing", i+1)
		}
		joined = q
	}
	if !strings.Contains(joined, "three") {
		t.Fatalf("streamed text never queued: %q", joined)
	}
	if strings.Contains(queuedAfterDelta[0], "three") {
		t.Fatalf("the whole answer queued on the first delta: %v", queuedAfterDelta)
	}
}
