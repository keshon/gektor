package run

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/roles"
	"github.com/keshon/tars/internal/tools"
	"github.com/keshon/tars/internal/workspace"
)

// streamingClient answers unary and streams, so a session that was built
// with Stream on must take the streaming path and emit deltas.
type streamingClient struct {
	calls int
	deltas int
}

func (c *streamingClient) Chat(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	c.calls++
	return llm.ChatResponse{
		Message:      llm.Message{Role: llm.RoleAssistant, Content: "one two three"},
		FinishReason: "stop",
	}, nil
}

func (c *streamingClient) Stream(_ context.Context, _ llm.ChatRequest, _, onContent func(string)) (llm.ChatResponse, error) {
	c.calls++
	for _, chunk := range []string{"one ", "two ", "three"} {
		c.deltas++
		onContent(chunk)
	}
	return llm.ChatResponse{
		Message:      llm.Message{Role: llm.RoleAssistant, Content: "one two three"},
		FinishReason: "stop",
	}, nil
}

// A session configured to stream must actually stream. The TUI's live
// reveal is driven entirely by these events, and nothing else emits them,
// so a run that takes the unary path shows the whole reply at once.
func TestSession_StreamsWhenConfigured(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := &streamingClient{}
	var streamed []string
	sess, err := New(Config{
		Task: "task",
		Env: roles.Env{
			Client: client,
			WS:     ws,
			Procs:  tools.NewBackgroundProcesses(),
			Stream: true,
		},
		OnEvent: func(ev Event) {
			if ev.Name == "delta" {
				streamed = append(streamed, ev.Fields["text"].(string))
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.deltas == 0 {
		t.Fatal("Stream was never called")
	}
	if len(streamed) == 0 {
		t.Fatal("no delta events reached OnEvent")
	}
	if got := strings.Join(streamed, ""); got != "one two three" {
		t.Fatalf("streamed text = %q", got)
	}
}

// Without Stream the session must take the unary path and emit no deltas.
func TestSession_UnaryWhenNotConfiguredToStream(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	client := &streamingClient{}
	streamed := 0
	sess, err := New(Config{
		Task: "task",
		Env: roles.Env{
			Client: client,
			WS:     ws,
			Procs:  tools.NewBackgroundProcesses(),
		},
		OnEvent: func(ev Event) {
			if ev.Name == "delta" {
				streamed++
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if client.deltas != 0 {
		t.Fatalf("streamed %d times without Stream set", client.deltas)
	}
	if streamed != 0 {
		t.Fatalf("%d delta events emitted on the unary path", streamed)
	}
}

// Delta events must carry the text under the key the TUI reads.
func TestSession_DeltaEventShape(t *testing.T) {
	line, _ := json.Marshal(map[string]any{"event": "delta", "seq": 1, "text": "hi"})
	if !strings.Contains(string(line), `"text":"hi"`) {
		t.Fatalf("delta payload shape changed: %s", line)
	}
}