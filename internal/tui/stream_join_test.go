package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/tars/internal/events"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/roles"
	"github.com/keshon/tars/internal/run"
	"github.com/keshon/tars/internal/tools"
	"github.com/keshon/tars/internal/workspace"
)

func TestHTTPStreamShowsThinkingBeforeResponseFinishes(t *testing.T) {
	textGate, finishGate := make(chan struct{}), make(chan struct{})
	var textOnce, finishOnce sync.Once
	allowText := func() { textOnce.Do(func() { close(textGate) }) }
	allowFinish := func() { finishOnce.Do(func() { close(finishGate) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/props" {
			fmt.Fprint(w, `{"model_path":"/models/test.gguf","default_generation_settings":{"n_ctx":8192}}`)
			return
		}
		var req struct {
			ReturnProgress bool `json:"return_progress"`
			Timings        bool `json:"timings_per_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if !req.ReturnProgress || !req.Timings {
			t.Error("llama request omitted live telemetry")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		frame := func(s string) { fmt.Fprint(w, "data: "+s+"\n\n"); w.(http.Flusher).Flush() }
		frame(`{"choices":[{"delta":{"role":"assistant"}}],"prompt_progress":{"total":1000,"cache":600,"processed":800},"timings":{"prompt_n":200,"cache_n":600,"predicted_n":0}}`)
		frame(`{"choices":[{"delta":{"reasoning_content":"checking parser"}}],"timings":{"prompt_n":400,"cache_n":600,"predicted_n":3}}`)
		select {
		case <-textGate:
		case <-r.Context().Done():
			return
		}
		frame(`{"choices":[{"delta":{"content":"hello"}}],"timings":{"prompt_n":400,"cache_n":600,"predicted_n":5}}`)
		select {
		case <-finishGate:
		case <-r.Context().Done():
			return
		}
		frame(`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":600}}}`)
		frame(`[DONE]`)
	}))
	defer srv.Close()
	defer allowFinish()
	defer allowText()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	queue := make(chan run.Event, 32)
	sess, err := run.New(run.Config{Task: "say hello", Plan: true, Env: roles.Env{Client: llm.NewLlamaClient(srv.URL, "local"), WS: ws, BackendKind: "llama", Stream: true}, OnEvent: func(ev run.Event) { queue <- ev }})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := sess.Run(ctx); done <- err }()
	m := sizeModel(t, testModel())
	m.state = stRunning
	consumeUntil := func(phase string) {
		t.Helper()
		for {
			select {
			case ev := <-queue:
				m.handleEvent(ev)
				if ev.Name == "progress" && m.progress.Phase == phase {
					return
				}
			case <-ctx.Done():
				t.Fatal("stream stopped before " + phase)
			}
		}
	}
	consumeUntil("prefill")
	if m.tokens != 1000 || m.tokensEst || !strings.Contains(m.badgeLine(), "READ 80%") {
		t.Fatalf("prefill header: %q", m.badgeLine())
	}
	consumeUntil("reasoning")
	if !m.liveFlushPending || len(m.pendingCommands) == 0 {
		t.Fatal("no frame scheduled for thinking")
	}
	// Run the actual frame timer, independent of the spinner/idle heartbeat.
	msg := m.pendingCommands[len(m.pendingCommands)-1]()
	m.Update(msg)
	if m.live != "" || !strings.Contains(m.View().Content, "checking parser") {
		t.Fatal("thinking is hidden until response starts")
	}
	allowText()
	consumeUntil("generating")
	if m.tokens != 1005 || m.tokensEst {
		t.Fatalf("cached input/output counted incorrectly: %d estimated=%v", m.tokens, m.tokensEst)
	}
	m.Update(m.pendingCommands[len(m.pendingCommands)-1]())
	if m.live != "hello" || m.liveThink != "checking parser" || m.liveQueue != "" {
		t.Fatalf("frame did not show complete chunks: %q / %q", m.liveThink, m.live)
	}
	select {
	case err := <-done:
		t.Fatalf("server should still be generating, got %v", err)
	default:
	}
	allowFinish()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("stream never finished")
	}
}

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
				queuedAfterDelta = append(queuedAfterDelta, m.live+m.liveQueue)
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
