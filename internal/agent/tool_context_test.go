package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/llm"
)

type toolContextKey struct{}
type contextProbe struct {
	t      *testing.T
	budget bool
	called *bool
}

func (contextProbe) Name() string            { return "probe_context" }
func (contextProbe) Description() string     { return "probe" }
func (contextProbe) Mode() ToolMode          { return Concurrent }
func (contextProbe) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (p contextProbe) Run(ctx context.Context, _ json.RawMessage) (string, error) {
	*p.called = true
	if ctx.Value(toolContextKey{}) != "vision-and-output" {
		p.t.Error("tool execution lost context metadata")
	}
	if _, ok := ctx.Deadline(); ok != p.budget {
		p.t.Error("tool execution lost work deadline")
	}
	return "observed", nil
}

func TestToolContextReachesExecutionWithAndWithoutRunBudget(t *testing.T) {
	for _, limit := range []time.Duration{0, 5 * time.Second} {
		t.Run(limit.String(), func(t *testing.T) {
			called, enriched := false, 0
			client := &stubClient{responses: []llm.ChatResponse{toolCallStep("probe_context"), {Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}}}}
			a := New(Config{Client: client, Tools: NewRegistry(contextProbe{t, limit > 0, &called}), RunTimeout: limit, ToolContext: func(ctx context.Context) context.Context {
				enriched++
				return context.WithValue(ctx, toolContextKey{}, "vision-and-output")
			}})
			if _, err := a.Run(t.Context(), "observe"); err != nil {
				t.Fatal(err)
			}
			if !called || enriched != 1 {
				t.Fatal(called, enriched)
			}
		})
	}
}
