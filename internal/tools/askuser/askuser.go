package askuser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/agent"
)

// Tool blocks until the user supplies an answer via AskFn, which the
// CLI implements as a simple stdin readline. The tool itself knows nothing
// about stdin — AskFn is the seam that lets a future web API (or test)
// implement the same interaction differently without touching this code.
//
// Subagents must NOT get this tool: delegate_task's contract is a fully-
// specified, self-contained subtask. If a subtask is ambiguous enough to
// need user input it wasn't well-specified for delegation. Practically,
// multiple concurrent subagents sharing a single stdin channel would race
// each other.
type Tool struct {
	// AskFn is the actual blocking I/O. It receives the question and
	// returns the user's answer. The CLI uses bufio.NewReader(os.Stdin).
	AskFn func(context.Context, string) (string, error)
}

func (Tool) Name() string { return "ask_user" }

// Mode is Exclusive: blocks the entire process; must never run alongside
// anything else in the same step's concurrent batch.
func (Tool) Mode() agent.ToolMode { return agent.Exclusive }

func (Tool) Description() string {
	return "Pause for one consequential clarification or manual browser login/MFA. Ask for completion, never passwords. Assume minor choices. Not available to delegated subtasks."
}

func (Tool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"question": {"type": "string"}
		},
		"required": ["question"]
	}`)
}

// Question reads the question back out of a call's arguments, so the loop
// can find one left unanswered in history and answer it on resume without
// knowing this tool's argument shape.
func (Tool) Question(args json.RawMessage) string {
	var in struct {
		Question string `json:"question"`
	}
	if json.Unmarshal(args, &in) != nil {
		return ""
	}
	return strings.TrimSpace(in.Question)
}

func (t Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if t.AskFn == nil {
		return "", fmt.Errorf("ask_user: no AskFn configured")
	}
	question := Tool{}.Question(args)
	if question == "" {
		return "", fmt.Errorf("ask_user: no question given")
	}
	answer, err := t.AskFn(ctx, question)
	if err != nil {
		return "", fmt.Errorf("ask_user: %w", err)
	}
	return answer, nil
}
