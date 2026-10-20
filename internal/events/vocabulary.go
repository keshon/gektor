package events

import (
	"bytes"
	"encoding/json"
	"io"
)

// The wire vocabulary. Every JSONL event that needs nothing but strings
// is declared here beside the Emitter that writes it, so a new event kind
// and its schema cannot land in different packages.
//
// These used to live in internal/api, which put the wire format on the far
// side of the run driver from the emitter that writes it. EmitStep and
// EmitUsage stayed with the run: they take llm types, and this package is
// a leaf by an enforced invariant (docs/conventions.md, package-layers).

// Event is one decoded JSONL event: the name plus its fields, including
// any emitted alongside it.
type Event struct {
	Name   string
	Seq    int
	Fields map[string]any
}

// callbackWriter decodes each JSONL line back into an onEvent call,
// preserving the emitter-assigned sequence numbers. Serializing through
// the emitter's own encoding keeps -mode json output and in-process
// events on one schema by construction: they cannot drift apart.
type callbackWriter struct {
	buf     bytes.Buffer
	onEvent func(Event)
}

func (w *callbackWriter) Write(p []byte) (int, error) {
	n, err := w.buf.Write(p)
	if err != nil {
		return n, err
	}
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			break
		}
		var rec map[string]any
		if jerr := json.Unmarshal([]byte(line), &rec); jerr != nil {
			continue
		}
		name, _ := rec["event"].(string)
		ev := Event{Name: name, Fields: map[string]any{}}
		for k, v := range rec {
			switch k {
			case "event":
			case "seq":
				if f, ok := v.(float64); ok {
					ev.Seq = int(f)
				}
			default:
				ev.Fields[k] = v
			}
		}
		if w.onEvent != nil {
			w.onEvent(ev)
		}
	}
	return n, nil
}

// EventWriter adapts the shared JSONL emitter to an in-process event sink.
func EventWriter(onEvent func(Event)) io.Writer { return &callbackWriter{onEvent: onEvent} }

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
