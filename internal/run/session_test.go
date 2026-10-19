package run

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/checks"
	"github.com/keshon/tars/internal/events"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/roles"
	"github.com/keshon/tars/internal/tools"
	"github.com/keshon/tars/internal/workspace"
)

func testEnv(t *testing.T, client llm.Client, record func(string, int, llm.Message)) roles.Env {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return roles.Env{
		Client: client, WS: ws, Procs: tools.NewBackgroundProcesses(),
		OnStep: record,
	}
}

func TestNew_Validates(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("empty task must fail")
	}
	ws, _ := workspace.New(t.TempDir())
	if _, err := New(Config{Task: "x", Env: roles.Env{WS: ws}}); err == nil {
		t.Fatal("nil client must fail")
	}
	if _, err := New(Config{Task: "x", Env: roles.Env{Client: stubOK{}, WS: ws}}); err != nil {
		t.Fatalf("valid config: %v", err)
	}
}

type stubOK struct{}

func (stubOK) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

// writeThenDone drives one real file write, then answers: sessions
// under test need tool activity for the verify round to fire.
type writeThenDone struct {
	n int
}

func (w *writeThenDone) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	w.n++
	if w.n == 1 {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "write_file", Arguments: json.RawMessage(`{"path":"note.txt","content":"hi"}`)},
		}}}, nil
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

func TestSession_RunEmitsSameSchemaAsEmitter(t *testing.T) {
	var events []Event
	s, err := New(Config{
		Task: "task",
		Env:  testEnv(t, &writeThenDone{}, nil),
		OnEvent: func(ev Event) {
			events = append(events, ev)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "done" {
		t.Fatalf("result = %q", out)
	}
	// Framing is the transport's job: Session streams steps, results,
	// usage, and harness nudges — nothing else. (This run writes a
	// file first, so tool_result appears too — all four are known
	// vocabulary. The stub reports zero usage, so usage events carry
	// zeros.)
	seenStep, seenUsage, seenNudge, seenOutcome := false, false, false, false
	lastSeq := 0
	for _, ev := range events {
		if ev.Seq <= lastSeq {
			t.Fatalf("event sequence regressed: %d after %d", ev.Seq, lastSeq)
		}
		lastSeq = ev.Seq
		switch ev.Name {
		case "step":
			seenStep = true
		case "usage":
			seenUsage = true
			if _, ok := ev.Fields["prompt"]; !ok {
				t.Fatalf("usage without prompt: %+v", ev)
			}
		case "tool_result":
			if _, ok := ev.Fields["call_id"]; !ok {
				t.Fatalf("tool_result without call_id: %+v", ev)
			}
		case "nudge":
			seenNudge = true
			if _, ok := ev.Fields["kind"]; !ok {
				t.Fatalf("nudge without kind: %+v", ev)
			}
			if text, _ := ev.Fields["text"].(string); !strings.HasPrefix(text, "[harness] ") {
				t.Fatalf("nudge without provenance: %+v", ev)
			}
		case "outcome":
			seenOutcome = true
			if ev.Fields["outcome"] != "completed" {
				t.Fatalf("unexpected outcome: %+v", ev)
			}
			files, _ := ev.Fields["files"].([]any)
			if len(files) != 1 || files[0] != "note.txt" {
				t.Fatalf("expected observed file effect: %+v", ev)
			}
		default:
			t.Fatalf("events = %+v, want known vocabulary only", events)
		}
	}
	if !seenStep || !seenUsage || !seenOutcome {
		t.Fatalf("steps and usage both expected: %+v", events)
	}
	if seenNudge {
		t.Fatalf("ordinary completion injected an unsolicited retry: %+v", events)
	}
}

func TestSession_AskGoesThroughSuspender(t *testing.T) {
	asked := 0
	client := &askThenDone{}
	s, err := New(Config{
		Task: "task",
		Env:  testEnv(t, client, nil),
		Answer: func(_ context.Context, req agent.SuspendRequest) (agent.SuspendReply, error) {
			if req.Kind != agent.SuspendAsk {
				t.Errorf("kind = %s", req.Kind)
			}
			asked++
			return agent.SuspendReply{Answer: "assumed"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if asked != 1 {
		t.Fatalf("ask suspender called %dx", asked)
	}
}

type askThenDone struct{ calls int }

func (s *askThenDone) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	s.calls++
	if s.calls == 1 {
		return llm.ChatResponse{Message: llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{{
				ID: "q1", Name: "ask_user",
				Arguments: json.RawMessage(`{"question":"which?"}`),
			}},
		}}, nil
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

func TestSession_HeadlessFailsClosed(t *testing.T) {
	s, err := New(Config{
		Task: "task",
		Env:  testEnv(t, &askThenDone{}, nil),
		// No Answer: ClosedSuspender must fail, never hang.
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.Run(context.Background())
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("headless run hung instead of failing closed")
	}
}

func TestSession_HarnessReplyFlaggedAfterNudge(t *testing.T) {
	var events []Event
	s, err := New(Config{
		Task: "task",
		Env:  testEnv(t, &seqClient{steps: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, Content: "partial"}, FinishReason: "length"}, budgetSay("done")}}, nil),
		OnEvent: func(ev Event) {
			events = append(events, ev)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The step right after a nudge carries the flag when text-only.
	nudgeAt := -1
	for i, ev := range events {
		if ev.Name == "nudge" {
			nudgeAt = i
			break
		}
	}
	if nudgeAt < 0 {
		t.Fatalf("no nudge in %d events", len(events))
	}
	found := false
	for _, ev := range events[nudgeAt+1:] {
		if ev.Name != "step" {
			continue
		}
		if ev.Fields["harness_reply"] != true {
			t.Fatalf("post-nudge step missing flag: %+v", ev.Fields)
		}
		found = true
		break
	}
	if !found {
		t.Fatal("no step followed the nudge")
	}
}

// EmitStep output must parse back field-identical through the event sink:
// -mode json and an in-process Session share one schema by construction.
func TestEmitStep_MatchesEmitterEncoding(t *testing.T) {
	msg := llm.Message{
		Role:    llm.RoleAssistant,
		Content: "working",
		ToolCalls: []llm.ToolCall{
			{ID: "c1", Name: "read_file", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
		},
	}
	var buf bytes.Buffer
	EmitStep(events.New(&buf), "w1", 3, msg, false, 0)

	var got []events.Event
	if _, err := events.EventWriter(func(ev events.Event) { got = append(got, ev) }).Write(buf.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(got) != 1 || got[0].Name != "step" {
		t.Fatalf("events = %+v", got)
	}
	ev := got[0]
	if ev.Fields["step"].(float64) != 3 || ev.Fields["label"] != "w1" || ev.Fields["text"] != "working" {
		t.Fatalf("fields = %+v", ev.Fields)
	}
	calls, ok := ev.Fields["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls = %+v", ev.Fields["tool_calls"])
	}
	c := calls[0].(map[string]any)
	if c["id"] != "c1" || c["name"] != "read_file" || c["args"] != `{"path":"a.txt"}` {
		t.Fatalf("call = %+v", c)
	}
}

func TestEmitStep_IncludesCallID(t *testing.T) {
	var buf bytes.Buffer
	EmitStep(events.New(&buf), "run", 1, llm.Message{
		Content: "x",
		ToolCalls: []llm.ToolCall{
			{ID: "a", Name: "read_file", Arguments: json.RawMessage(`{"path":"a"}`)},
			{ID: "b", Name: "read_file", Arguments: json.RawMessage(`{"path":"b"}`)},
		},
	}, false, 0)
	calls := strings.Count(buf.String(), `"id":`)
	if calls != 2 {
		t.Fatalf("ids in output = %d, want 2: %s", calls, buf.String())
	}
}

func TestEmitStep_HarnessReplyFlag(t *testing.T) {
	var on, off bytes.Buffer
	EmitStep(events.New(&on), "run", 1, llm.Message{Content: "hi"}, true, 0)
	EmitStep(events.New(&off), "run", 1, llm.Message{Content: "hi"}, false, 0)
	if !strings.Contains(on.String(), `"harness_reply":true`) {
		t.Fatalf("flag missing: %s", on.String())
	}
	if strings.Contains(off.String(), "harness_reply") {
		t.Fatalf("false flag must be omitted entirely: %s", off.String())
	}
}

func TestEmitUsage_RoundTrips(t *testing.T) {
	var buf bytes.Buffer
	EmitUsage(events.New(&buf), 4, llm.Usage{PromptTokens: 12400, CompletionTokens: 300, CachedTokens: 2000})
	var got []events.Event
	if _, err := events.EventWriter(func(ev events.Event) { got = append(got, ev) }).Write(buf.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(got) != 1 || got[0].Name != "usage" {
		t.Fatalf("events = %+v", got)
	}
	f := got[0].Fields
	if f["step"].(float64) != 4 || f["prompt"].(float64) != 12400 || f["completion"].(float64) != 300 {
		t.Fatalf("fields = %+v", f)
	}
}

type sideEffectClient struct {
	root  string
	calls int
}

func (c *sideEffectClient) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	c.calls++
	if c.calls == 1 {
		if err := os.WriteFile(filepath.Join(c.root, "stray.txt"), []byte("-----BEGIN PRIVATE KEY-----"), 0600); err != nil {
			return llm.ChatResponse{}, err
		}
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "list", Name: "list_files", Arguments: json.RawMessage(`{"path":"."}`)}}}}, nil
	}
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}, nil
}

func TestSessionSweepUsesTheEventReporter(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprint(resume), func(t *testing.T) {
			env := testEnv(t, stubOK{}, nil)
			env.Client = &sideEffectClient{root: env.WS.Root()}

			env.Rules = checks.Secrets()
			old, wire := 0, 0
			env.Checks = checks.NewReporter(env.Rules, func(checks.Scope, checks.Finding) { old++ })
			s, err := New(Config{Task: "task", Env: env, OnEvent: func(e Event) {
				if e.Name == "finding" {
					wire++
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			if resume {
				_, err = s.Resume(t.Context(), []llm.Message{{Role: llm.RoleSystem}, {Role: llm.RoleUser, Content: "task"}}, "continue")
			} else {
				_, err = s.Run(t.Context())
			}
			if err != nil {
				t.Fatal(err)
			}
			if old != 0 || wire != 1 {
				t.Fatalf("old sink=%d event findings=%d", old, wire)
			}
		})
	}
}
