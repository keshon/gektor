package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/config"
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
	Spawn        func(ctx context.Context, role string, steps int) *agent.Agent
	DefaultSteps int
	MaxSteps     int
}

func (*Delegate) Name() string { return "delegate_task" }

// Delegates share a workspace, so each one runs without overlapping parent
// tools. That also means the scheduler never runs two at once, so a
// semaphore here would only ever admit one.
func (*Delegate) Mode() agent.ToolMode { return agent.Exclusive }
func (d *Delegate) Description() string {
	steps, maximum, _ := d.budget()
	return fmt.Sprintf("Delegate independent work; handle coupled edits yourself. Supply paths/requirements; no parent history. Shared workspace; sequential, no nesting. Budget %d model steps, max %d, final answer included. Optional max_steps. Inspect partial results.", steps, maximum)
}
func (d *Delegate) Schema() json.RawMessage {
	_, maximum, _ := d.budget()
	return json.RawMessage(fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"task": {"type": "string"},
			"role": {"type": "string"},
			"max_steps": {"type": "integer", "minimum": 1, "maximum": %d}
		},
		"required": ["task"], "additionalProperties": false
	}`, maximum))
}

func (d *Delegate) budget() (int, int, error) {
	return (config.Harness{DelegateSteps: d.DefaultSteps, DelegateMaxSteps: d.MaxSteps}).DelegateBudget()
}

func (d *Delegate) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Task     string `json:"task"`
		Role     string `json:"role"`
		MaxSteps *int   `json:"max_steps"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	if err := rejectUnknownFields(args, "task", "role", "max_steps"); err != nil {
		return "", err
	}
	steps, maximum, err := d.budget()
	if err != nil {
		return "", err
	}
	if in.MaxSteps != nil {
		if *in.MaxSteps < 1 || *in.MaxSteps > maximum {
			return "", fmt.Errorf("max_steps must be between 1 and %d", maximum)
		}
		steps = *in.MaxSteps
	}
	if strings.TrimSpace(in.Task) == "" {
		return "", fmt.Errorf("task is required")
	}

	started := time.Now()
	sub := d.Spawn(ctx, in.Role, steps)
	result, err := sub.Run(ctx, in.Task)
	if err == nil && strings.TrimSpace(result) == "" {
		err = fmt.Errorf("subagent returned empty result")
	}
	out := formatDelegateReport(sub.Report(), sub.LastRunMutations, result, err, time.Since(started))
	if id := agent.ToolCallID(ctx); id != "" {
		out = strings.Replace(out, "DELEGATE\n", "DELEGATE\ndelegate_call_id: "+reportLine(id)+"\n", 1)
	}
	return out, err
}

func formatDelegateReport(report agent.RunReport, mutations int, result string, runErr error, elapsed time.Duration) string {
	var b strings.Builder
	b.WriteString("DELEGATE\n")
	fmt.Fprintf(&b, "mutations: %d\n", mutations)
	// Keep the mutation header compatible with parent fallback accounting.
	for i, p := range report.MutatedPaths {
		if i == 32 {
			fmt.Fprintf(&b, "paths_omitted: %d\n", len(report.MutatedPaths)-i)
			break
		}
		fmt.Fprintf(&b, "paths: %s\n", reportLine(p))
	}
	reason := "completed"
	switch {
	case errors.Is(runErr, context.Canceled):
		reason = "cancelled"
	case errors.Is(runErr, context.DeadlineExceeded):
		reason = "deadline_exceeded"
	case errors.Is(runErr, agent.ErrMaxSteps):
		reason = "step_limit"
	case runErr != nil:
		reason = "failed"
	}
	fmt.Fprintf(&b, "termination: %s\nsteps: %d/%d\nelapsed_ms: %d\ntools_executed: %d\n", reason, report.Steps, report.MaxSteps, elapsed.Milliseconds(), report.ToolsExecuted)
	fmt.Fprintf(&b, "tokens: prompt=%d completion=%d cached=%d estimated=%t\n", report.Usage.PromptTokens, report.Usage.CompletionTokens, report.Usage.CachedTokens, report.Usage.Estimated)
	if report.Archive != "" {
		fmt.Fprintf(&b, "archive: %s\n", reportLine(report.Archive))
	}
	for i, warning := range report.Warnings {
		if i == 4 {
			break
		}
		fmt.Fprintf(&b, "warning: %s\n", reportLine(warning))
	}
	for _, tool := range report.RecentTools {
		fmt.Fprintf(&b, "tool_result: %s %s %s\n", agent.TruncateMiddle(reportLine(tool.CallID), 128), agent.TruncateMiddle(reportLine(tool.Name), 64), agent.TruncateMiddle(reportLine(tool.Status), 32))
	}
	if report.Verification != "" {
		fmt.Fprintf(&b, "recorded_verification: %s\n", agent.TruncateMiddle(report.Verification, 2048))
	}
	b.WriteString("----\n")
	if report.Summary != "" {
		fmt.Fprintf(&b, "Model partial summary (unverified):\n%s\n", agent.TruncateMiddle(report.Summary, 2048))
	}
	if len(report.OpenTodos) > 0 {
		fmt.Fprintf(&b, "Model remaining work (unverified):\n%s\n", agent.TruncateMiddle(strings.Join(report.OpenTodos, "\n"), 2048))
	}
	if result != "" {
		b.WriteString("Model answer (unverified):\n" + agent.TruncateMiddle(result, 8192))
	}
	return b.String()
}

func reportLine(s string) string {
	return agent.TruncateMiddle(strings.NewReplacer("\r", " ", "\n", " ").Replace(s), 512)
}
