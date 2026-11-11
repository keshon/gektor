package roles

import (
	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
)

// childObserver binds one parent call ID to child-only observations. It borrows
// the destination sink; the parent owns that sink's lifetime and serialization.
type childObserver struct {
	callID string
	sink   events.Sink
}

func observeSubagent(cfg *agent.Config, env Env, callID string) {
	observer := &childObserver{callID: callID, sink: env.ChildEvents}
	cfg.OnStep = observer.step
	cfg.OnResult = observer.result
	cfg.OnToolOutput = observer.toolOutput
	cfg.OnNudge = observer.nudge
	cfg.OnProgress = observer.progress
	cfg.OnLocal = observer.local
	cfg.Stream = env.Stream
	cfg.OnDelta = observer.delta
	cfg.OnReasoning = observer.reasoning
}

func (o *childObserver) emit(kind string, fields map[string]any) {
	event := events.Event{
		Name: "child",
		Fields: map[string]any{
			"call_id": o.callID,
			"kind":    kind,
			"fields":  fields,
		},
	}
	o.sink.Handle(event)
}

func (o *childObserver) step(_ int, message llm.Message) {
	o.emit("message", map[string]any{"message": message})
}

func (o *childObserver) result(result agent.ToolResult) {
	failed := result.Failed
	message := llm.Message{
		Role:       llm.RoleTool,
		ToolCallID: result.CallID,
		Content:    result.Output,
		ToolFailed: &failed,
	}
	o.emit("message", map[string]any{"message": message})
}

func (o *childObserver) toolOutput(callID, text string) {
	o.emit("tool_output", map[string]any{"tool_call_id": callID, "text": text})
}

func (o *childObserver) nudge(_ string, text string) {
	message := llm.Message{Role: llm.RoleUser, Content: "[harness] " + text}
	o.emit("message", map[string]any{"message": message})
}

func (o *childObserver) progress(progress llm.Progress) {
	o.emit("progress", map[string]any{"phase": progress.Phase, "detail": progress.Detail})
}

func (o *childObserver) local(text string) {
	o.emit("notice", map[string]any{"text": text})
}

func (o *childObserver) delta(text string) {
	o.emit("delta", map[string]any{"text": text})
}

func (o *childObserver) reasoning(text string) {
	o.emit("reasoning", map[string]any{"text": text})
}
