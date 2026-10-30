package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/keshon/gektor/internal/agent"
)

// Delegate lets an agent hand off a self-contained subtask to a fresh
// subagent and get back its final answer. This is the entire
// "orchestrator" from agent-v3, replaced by one tool: delegation doesn't
// need its own architecture, just a tool that recursively builds and runs
// another *agent.Agent.
type Delegate struct {
	// Spawn builds a fresh subagent for one subtask. role, if non-empty,
	// is layered onto the subagent's base system prompt rather than
	// replacing it — the caller still gets the baseline tool-usage rules
	// (file tools, validation, etc); the role specializes its task focus.
	// Spawn typically shares the same Client and Workspace but a smaller,
	// task-specific tool set — in particular, without Delegate itself, so
	// a task can't recurse into subagents forever.
	Spawn func(role string) *agent.Agent
}

func (*Delegate) Name() string { return "delegate_task" }

// Delegates share a workspace, so each one runs without overlapping parent
// tools. That also means the scheduler never runs two at once, so a
// semaphore here would only ever admit one.
func (*Delegate) Mode() agent.ToolMode { return agent.Exclusive }
func (*Delegate) Description() string {
	return "Delegate self-contained work worth a fresh context; do small/coupled work directly. Supply requirements and paths: parent history is absent. Optional role sets focus. Shares the workspace; runs sequentially with a step limit and no further delegation. Verify returned changes and evidence."
}
func (*Delegate) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"task": {"type": "string"},
			"role": {"type": "string"}
		},
		"required": ["task"]
	}`)
}

func (d *Delegate) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Task string `json:"task"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}

	sub := d.Spawn(in.Role)
	result, err := sub.Run(ctx, in.Task)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("subagent returned empty result")
	}
	return formatDelegateResult(sub.LastRunMutations, sub.MutatedPaths(), result), nil
}

func formatDelegateResult(mutations int, paths []string, result string) string {
	var b strings.Builder
	b.WriteString("DELEGATE\n")
	fmt.Fprintf(&b, "mutations: %d\n", mutations)
	for _, p := range paths {
		fmt.Fprintf(&b, "paths: %s\n", p)
	}
	b.WriteString("----\n")
	b.WriteString(result)
	return b.String()
}
