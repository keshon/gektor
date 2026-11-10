package events

// The wire vocabulary. Every JSONL event that needs nothing but strings
// is declared here beside the Emitter that writes it, so a new event kind
// and its schema cannot land in different packages.
//
// EmitStep and EmitUsage live in harness because they use llm types; this
// package remains independent of execution and provider contracts.

// Event carries native Go values in process. Wire adapters serialize the same
// name, sequence and fields as JSONL. Published nested payloads are immutable.
type Event struct {
	Name   string
	Seq    int
	Fields map[string]any
}

// EmitFinding writes one deterministic-check finding. Scope is "per-edit"
// (post-write scan) or "session-end" (deep sweep).
func EmitFinding(emitter *Emitter, scope, rule, path string, line int, summary string) {
	if emitter == nil {
		return
	}
	emitter.Emit("finding", map[string]any{
		"scope": scope, "rule": rule, "path": path, "line": line, "summary": summary,
	})
}

// EmitNudge writes one loop-generated harness notice: the same text the
// model saw (including its [harness] provenance mark), tagged by kind
// for display filtering.
func EmitNudge(emitter *Emitter, kind, text string) {
	if emitter == nil {
		return
	}
	emitter.Emit("nudge", map[string]any{"kind": kind, "text": text})
}

// EmitReasoning writes one streamed deliberation chunk. It is a separate
// event kind from delta so a reader can render THINK finishing before
// RESPONSE begins, rather than one buffer that only reveals everything
// once the turn is over.
func EmitReasoning(emitter *Emitter, text string) {
	if emitter == nil {
		return
	}
	emitter.Emit("reasoning", map[string]any{"text": text})
}

// EmitDelta writes one streamed content chunk. Deltas are display-only:
// the step event carries the authoritative text and replaces whatever the
// live buffer showed.
func EmitDelta(emitter *Emitter, text string) {
	if emitter == nil {
		return
	}
	emitter.Emit("delta", map[string]any{"text": text})
}

// EmitToolOutput writes a display-only chunk before the authoritative result.
func EmitToolOutput(emitter *Emitter, callID, text string) {
	if emitter != nil && text != "" {
		emitter.Emit("tool_output", map[string]any{"call_id": callID, "text": text})
	}
}

// EmitToolResult writes one completed tool call. Callers pair it with the
// matching tool_calls entry by call ID; the result text is capped like
// every other field.
func EmitToolResult(emitter *Emitter, callID string, result string, failed ...bool) {
	if emitter == nil {
		return
	}
	fields := map[string]any{"call_id": callID, "text": result}
	if len(failed) > 0 {
		fields["failed"] = failed[0]
	}
	emitter.Emit("tool_result", fields)
}

// EmitToolFinished reports execution completion before ordered result processing.
func EmitToolFinished(emitter *Emitter, callID string) {
	if emitter != nil {
		emitter.Emit("tool_finished", map[string]any{"call_id": callID})
	}
}

// EmitToolResultStatus preserves the execution status alongside the result.
func EmitToolResultStatus(emitter *Emitter, callID, output string, failed bool, status string) {
	if emitter != nil {
		emitter.Emit("tool_result", map[string]any{"call_id": callID, "text": output, "failed": failed, "status": status})
	}
}
