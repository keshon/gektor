package app_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/providers/openai"
)

// Opt-in: uses the signed-in account and its plan usage. All tool IO and saved
// history stay in temporary fixtures; ordinary tests never call the service.
func TestChatGPTLiveReasoning(t *testing.T) {
	model := os.Getenv("GEKTOR_LIVE_CHATGPT_MODEL")
	if model == "" {
		t.Skip("set GEKTOR_LIVE_CHATGPT_MODEL to enable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	client, err := app.NewBackend(ctx, app.BackendOptions{Kind: "chatgpt", URL: openai.API, Model: model, ReasoningEffort: "high", ReasoningSummary: true})
	if err != nil {
		t.Fatal(err)
	}
	var summary strings.Builder
	response, err := client.Stream(ctx, llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "How many permutations of 1 through 8 have no adjacent pair whose sum is 9? Work it out carefully and give the count with a brief explanation."}}}, func(s string) { summary.WriteString(s) }, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if response.Message.Content == "" {
		t.Fatal("empty answer")
	}
	if summary.Len() == 0 || response.Message.Reasoning != summary.String() {
		t.Fatal("missing or inconsistent streamed reasoning summary")
	}
	t.Logf("received %d summary bytes and %d answer bytes", summary.Len(), len(response.Message.Content))
}

func TestChatGPTLiveAcceptance(t *testing.T) {
	model := os.Getenv("GEKTOR_LIVE_CHATGPT_MODEL")
	if model == "" {
		t.Skip("set GEKTOR_LIVE_CHATGPT_MODEL to an available auth models ID")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	client, err := app.NewBackend(ctx, app.BackendOptions{Kind: "chatgpt", URL: openai.API, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	newConfig := func(path string, tools *agent.Registry) agent.Config {
		return agent.Config{Client: client, Tools: tools, System: "Follow the user's acceptance-test instructions precisely. Keep replies short unless asked otherwise.", StateFile: path, MaxSteps: 4, ContextLimit: 32768, Stream: true}
	}
	load := func(path string) conversation.Snapshot {
		t.Helper()
		s, err := conversation.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	t.Run("tool_round_trip_saved_replay_and_normalized_fallback", func(t *testing.T) {
		dir := t.TempDir()
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			t.Fatal(err)
		}
		marker := "fixture-" + hex.EncodeToString(random[:])
		fixture := &liveFixtureTool{path: filepath.Join(dir, "fixture.txt")}
		if err := os.WriteFile(fixture.path, []byte(marker), 0600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "state.json")
		cfg := newConfig(path, agent.NewRegistry(fixture))
		answer, err := agent.New(cfg).Run(ctx, "Call read_fixture once, then reply with exactly the returned marker. Do not guess it.")
		if err != nil || !strings.Contains(answer, marker) || fixture.calls.Load() != 1 {
			t.Fatalf("tool round trip: calls=%d marker_present=%t error=%v", fixture.calls.Load(), strings.Contains(answer, marker), err)
		}
		saved := load(path)
		native := 0
		for _, m := range saved.History {
			if m.ProviderState != nil && m.ProviderState.Matches(m) {
				native++
			}
		}
		if native == 0 {
			t.Fatal("completed live response did not save valid native replay state")
		}
		answer, err = agent.New(cfg).Resume(ctx, saved.Context(), "Without calling tools again, repeat the marker from the previous tool result.")
		if err != nil || !strings.Contains(answer, marker) || fixture.calls.Load() != 1 {
			t.Fatalf("saved native continuation: marker_present=%t calls=%d error=%v", strings.Contains(answer, marker), fixture.calls.Load(), err)
		}
		// Simulate an incompatible saved envelope while preserving the canonical
		// tool call and result. The adapter must send only normalized history.
		for i := range saved.History {
			if saved.History[i].ProviderState != nil {
				state := *saved.History[i].ProviderState
				state.Version++
				saved.History[i].ProviderState = &state
			}
		}
		cfg.StateFile = filepath.Join(dir, "fallback.json")
		if err := saved.Save(cfg.StateFile); err != nil {
			t.Fatal(err)
		}
		answer, err = agent.New(cfg).Resume(ctx, saved.Context(), "Without calling tools again, repeat the marker from the previous tool result.")
		if err != nil || !strings.Contains(answer, marker) || fixture.calls.Load() != 1 {
			t.Fatalf("normalized continuation: marker_present=%t calls=%d error=%v", strings.Contains(answer, marker), fixture.calls.Load(), err)
		}
		t.Logf("local tool executed once; %d valid replay envelopes saved; native and normalized continuations passed", native)
	})
	t.Run("cancel_partial_archive_and_resume", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cancelled.json")
		cfg := newConfig(path, agent.NewRegistry())
		cancelCtx, stop := context.WithCancel(ctx)
		defer stop()
		var visible strings.Builder
		cfg.OnDelta = func(text string) {
			visible.WriteString(text)
			if visible.Len() >= 24 {
				stop()
			}
		}
		_, err := agent.New(cfg).Run(cancelCtx, "Print the integers 1 through 300, each on its own line. No introduction or explanation.")
		if err == nil || visible.Len() < 24 || cancelCtx.Err() != context.Canceled {
			t.Fatalf("cancellation: visible_bytes=%d error=%v", visible.Len(), err)
		}
		saved := load(path)
		found := false
		for _, m := range saved.History {
			if m.Role == llm.RoleAssistant && m.Content == visible.String() {
				found = true
				if len(m.ToolCalls) != 0 || m.ProviderState != nil {
					t.Fatal("interrupted output retained executable calls or native state")
				}
			}
		}
		if !found {
			t.Fatal("visible partial answer was not archived exactly")
		}
		cfg.OnDelta = nil
		answer, err := agent.New(cfg).Resume(ctx, saved.Context(), "Stop counting. Reply exactly: RESUME_OK")
		if err != nil || !strings.Contains(answer, "RESUME_OK") {
			t.Fatalf("resume after cancellation: expected_marker=%t error=%v", strings.Contains(answer, "RESUME_OK"), err)
		}
		t.Logf("cancelled after %d visible bytes; partial archive and live resume passed", visible.Len())
	})
}

type liveFixtureTool struct {
	path  string
	calls atomic.Int32
}

func (*liveFixtureTool) Name() string        { return "read_fixture" }
func (*liveFixtureTool) Description() string { return "Read the isolated acceptance-test marker file." }
func (*liveFixtureTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (*liveFixtureTool) Mode() agent.ToolMode { return agent.Concurrent }
func (f *liveFixtureTool) Run(context.Context, json.RawMessage) (string, error) {
	f.calls.Add(1)
	data, err := os.ReadFile(f.path)
	return string(data), err
}
