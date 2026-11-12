package agentrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/roles"
	toolbackground "github.com/keshon/gektor/internal/tools/background"
	"github.com/keshon/gektor/internal/workspace"
)

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestServeReportsInputFailure(t *testing.T) {
	errInput := errors.New("input failed")
	deps := testServerConfig(t, &stubChat{})
	if err := Serve(t.Context(), deps, failingReader{errInput}, io.Discard); !errors.Is(err, errInput) {
		t.Fatal(err)
	}
}

func TestServeAcceptsNextRunAfterCompletionResponse(t *testing.T) {
	deps := testServerConfig(t, &stubChat{responses: []llm.ChatResponse{{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}}})
	in, writer := io.Pipe()
	out := &outLines{ch: make(chan string, 256)}
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), deps, in, out) }()
	t.Cleanup(func() {
		_ = writer.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("server did not drain")
		}
	})
	for id := 1; id <= 4; id++ {
		if _, err := fmt.Fprintf(writer, "{\"id\":%d,\"method\":\"run\",\"params\":{\"task\":\"next\"}}\n", id); err != nil {
			t.Fatal(err)
		}
		reply := out.waitFor(t, 5*time.Second, func(record map[string]any) bool { return record["id"] == float64(id) })
		if reply["error"] != nil || reply["result"] == nil {
			t.Fatal("completed run still occupied the server", reply)
		}
	}
}

type stubChat struct {
	mu        sync.Mutex
	responses []llm.ChatResponse
	calls     int
}

func (s *stubChat) Chat(_ context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.responses[s.calls%len(s.responses)]
	s.calls++
	return r, nil
}

func testServerConfig(t *testing.T, client llm.Client) Config {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	return Config{Run: harness.Config{Env: roles.Env{Client: client, WS: ws, Procs: toolbackground.New()}}, Directory: t.TempDir()}
}

// outLines collects stdout lines from a Serve under test.
type outLines struct {
	mu    sync.Mutex
	lines []string
	ch    chan string
}

func (o *outLines) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, line := range strings.Split(string(p), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		o.lines = append(o.lines, line)
		select {
		case o.ch <- line:
		default:
		}
	}
	return len(p), nil
}

func (o *outLines) waitFor(t *testing.T, timeout time.Duration, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		o.mu.Lock()
		lines := append([]string(nil), o.lines...)
		o.mu.Unlock()
		for _, line := range lines {
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				continue
			}
			if match(rec) {
				return rec
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for output; got %d lines: %v", len(lines), lines)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func runServe(t *testing.T, deps Config, script []string) *outLines {
	t.Helper()
	inR, inW := io.Pipe()
	out := &outLines{ch: make(chan string, 256)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Serve(context.Background(), deps, inR, out)
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Serve did not exit on stdin EOF")
		}
	})
	for _, line := range script {
		if _, err := io.WriteString(inW, line+"\n"); err != nil {
			t.Fatalf("write request: %v", err)
		}
	}
	return out
}

func TestServe_RunEndToEnd(t *testing.T) {
	stub := &stubChat{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	out := runServe(t, testServerConfig(t, stub),
		[]string{`{"id":1,"method":"run","params":{"task":"hi"}}`})

	rec := out.waitFor(t, 10*time.Second, func(rec map[string]any) bool {
		id, _ := rec["id"].(float64)
		_, hasResult := rec["result"]
		return id == 1 && hasResult
	})
	result, _ := rec["result"].(map[string]any)
	if result["answer"] != "done" {
		t.Fatalf("result = %+v", rec)
	}
	// The run announced itself before answering.
	out.waitFor(t, time.Second, func(rec map[string]any) bool {
		ev, _ := rec["event"].(string)
		return ev == "run_start"
	})
}

func TestServe_RespondAnswersGate(t *testing.T) {
	stub := &stubChat{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "q1", Name: "ask_user", Arguments: json.RawMessage(`{"question":"which?"}`)},
		}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
		// Twice: the verify round asks the model once more before
		// accepting the finish.
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	inR, inW := io.Pipe()
	out := &outLines{ch: make(chan string, 256)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Serve(context.Background(), testServerConfig(t, stub), inR, out)
	}()
	defer func() {
		_ = inW.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Serve did not exit on stdin EOF")
		}
	}()

	if _, err := io.WriteString(inW, "{\"id\":1,\"method\":\"run\",\"params\":{\"task\":\"hi\"}}\n"); err != nil {
		t.Fatal(err)
	}
	// The run must suspend on the question instead of touching stdin.
	out.waitFor(t, 10*time.Second, func(rec map[string]any) bool {
		ev, _ := rec["event"].(string)
		return ev == "awaiting_input"
	})
	if _, err := io.WriteString(inW, "{\"id\":2,\"method\":\"respond\",\"params\":{\"answer\":\"assumed\"}}\n"); err != nil {
		t.Fatal(err)
	}
	rec := out.waitFor(t, 10*time.Second, func(rec map[string]any) bool {
		id, _ := rec["id"].(float64)
		_, hasResult := rec["result"]
		return id == 1 && hasResult
	})
	if rec["result"].(map[string]any)["answer"] != "done" {
		t.Fatalf("result = %+v", rec)
	}
	// And the respond itself was acknowledged.
	out.waitFor(t, time.Second, func(rec map[string]any) bool {
		id, _ := rec["id"].(float64)
		_, hasResult := rec["result"]
		return id == 2 && hasResult
	})
}

func TestServe_RejectsSecondRun(t *testing.T) {
	hanging := &hangForever{}
	inR, inW := io.Pipe()
	out := &outLines{ch: make(chan string, 256)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Serve(context.Background(), testServerConfig(t, hanging), inR, out)
	}()
	defer func() {
		_ = inW.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("Serve did not exit on stdin EOF")
		}
	}()

	if _, err := io.WriteString(inW, "{\"id\":1,\"method\":\"run\",\"params\":{\"task\":\"slow\"}}\n"); err != nil {
		t.Fatal(err)
	}
	// Give the first run a moment to start, then a second run must be
	// refused — single-flight, never queued.
	deadline := time.Now().Add(5 * time.Second)
	for {
		out.mu.Lock()
		n := len(out.lines)
		out.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := io.WriteString(inW, "{\"id\":2,\"method\":\"run\",\"params\":{\"task\":\"other\"}}\n"); err != nil {
		t.Fatal(err)
	}
	rec := out.waitFor(t, 10*time.Second, func(rec map[string]any) bool {
		id, _ := rec["id"].(float64)
		_, hasErr := rec["error"]
		return id == 2 && hasErr
	})
	if !strings.Contains(rec["error"].(map[string]any)["message"].(string), "already in progress") {
		t.Fatalf("error = %+v", rec)
	}
	// Cancel the hung run so the server can shut down cleanly.
	if _, err := io.WriteString(inW, "{\"id\":3,\"method\":\"cancel\"}\n"); err != nil {
		t.Fatal(err)
	}
	out.waitFor(t, 10*time.Second, func(rec map[string]any) bool {
		id, _ := rec["id"].(float64)
		_, hasResult := rec["result"]
		return id == 3 && hasResult
	})
}

type hangForever struct{}

func (hangForever) Chat(ctx context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	<-ctx.Done()
	return llm.ChatResponse{}, ctx.Err()
}

func TestServe_EOFCancelsGatedRun(t *testing.T) {
	// A run suspended on a gate with closed stdin can never be answered:
	// EOF must cancel it (id-response error) instead of hanging shutdown.
	stub := &stubChat{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			{ID: "q1", Name: "ask_user", Arguments: json.RawMessage(`{"question":"which?"}`)},
		}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	inR, inW := io.Pipe()
	out := &outLines{ch: make(chan string, 256)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Serve(context.Background(), testServerConfig(t, stub), inR, out)
	}()

	if _, err := io.WriteString(inW, "{\"id\":1,\"method\":\"run\",\"params\":{\"task\":\"hi\"}}\n"); err != nil {
		t.Fatal(err)
	}
	// Wait until the run suspends, then close stdin with no answer.
	out.waitFor(t, 10*time.Second, func(rec map[string]any) bool {
		ev, _ := rec["event"].(string)
		return ev == "awaiting_input"
	})
	_ = inW.Close()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Serve hung on EOF with a gated run")
	}
	// The run must have failed (cancelled), not vanished: its id-response
	// carries the error.
	out.waitFor(t, time.Second, func(rec map[string]any) bool {
		id, _ := rec["id"].(float64)
		_, hasErr := rec["error"]
		return id == 1 && hasErr
	})
}

func TestServeEventSequenceIsMonotonic(t *testing.T) {
	stub := &stubChat{responses: []llm.ChatResponse{
		{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "q", Name: "ask_user", Arguments: json.RawMessage(`{"question":"which?"}`)}}}},
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	out := runServe(t, testServerConfig(t, stub), []string{`{"id":1,"method":"run","params":{"task":"ask"}}`})
	out.waitFor(t, 5*time.Second, func(rec map[string]any) bool { return rec["event"] == "awaiting_input" })
	out.mu.Lock()
	defer out.mu.Unlock()
	previous := 0
	for _, line := range out.lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		if _, ok := rec["event"]; !ok {
			continue
		}
		seq := int(rec["seq"].(float64))
		if seq != previous+1 {
			t.Fatalf("sequence %d after %d: %s", seq, previous, line)
		}
		previous = seq
	}
	if previous < 3 {
		t.Fatalf("too few events: %d", previous)
	}
}
