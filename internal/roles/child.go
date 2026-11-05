package roles

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/llm"
)

// Child observations never invoke the parent's step, usage, or stream hooks.
func observeSubagent(cfg *agent.Config, e Env, callID string) {
	emit := func(kind string, fields map[string]any) { e.OnChild(callID, kind, fields) }
	cfg.OnStep = func(step int, msg llm.Message) { emit("message", map[string]any{"message": msg}) }
	cfg.OnResult = func(result agent.ToolResult) {
		failed := result.Failed
		emit("message", map[string]any{"message": llm.Message{Role: llm.RoleTool, ToolCallID: result.CallID, Content: result.Output, ToolFailed: &failed}})
	}
	cfg.OnToolOutput = func(id, text string) { emit("tool_output", map[string]any{"tool_call_id": id, "text": text}) }
	cfg.OnNudge = func(kind, text string) {
		emit("message", map[string]any{"message": llm.Message{Role: llm.RoleUser, Content: "[harness] " + text}})
	}
	cfg.OnProgress = func(p llm.Progress) { emit("progress", map[string]any{"phase": p.Phase, "detail": p.Detail}) }
	cfg.OnLocal = func(text string) { emit("notice", map[string]any{"text": text}) }
	cfg.Stream = e.Stream
	cfg.OnDelta = func(text string) { emit("delta", map[string]any{"text": text}) }
	cfg.OnReasoning = func(text string) { emit("reasoning", map[string]any{"text": text}) }
}
