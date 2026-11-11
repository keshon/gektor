// Package events delivers ordered run observations to in-process consumers or
// a JSONL writer at a wire boundary.
package events

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Sink consumes ordered event values. Handle runs synchronously; implementations
// must not mutate retained payloads or call back into their publishing emitter.
type Sink interface {
	Handle(Event)
}

// Emitter delivers events to a sink or JSONL writer. A zero Emitter drops
// everything, so callers never branch on whether output is enabled. Emit is safe
// for concurrent publishers. The caller owns the retained writer/sink lifetime;
// Emitter never closes either.
type Emitter struct {
	// mu serializes delivery and guards seq.
	mu   sync.Mutex
	w    io.Writer
	sink Sink
	seq  int
}

// New returns an Emitter borrowing w for JSONL delivery.
func New(w io.Writer) *Emitter { return &Emitter{w: w} }

// NewSink delivers immutable event values directly in process. Publishers must
// not mutate nested payloads after emitting them; Emit copies the fields map.
// The caller owns sink cleanup after publishers have stopped.
func NewSink(sink Sink) *Emitter { return &Emitter{sink: sink} }

// Emit delivers one event with an auto-incrementing seq. Writer errors are
// dropped: a broken pipe must not abort a run. Sinks run synchronously in order
// and must not call back into this emitter.
func (e *Emitter) Emit(kind string, fields map[string]any) {
	if e == nil || e.w == nil && e.sink == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	if e.sink != nil {
		payload := make(map[string]any, len(fields))
		for key, value := range fields {
			payload[key] = value
		}
		e.sink.Handle(Event{Name: kind, Seq: e.seq, Fields: payload})
		return
	}
	rec := map[string]any{"seq": e.seq, "event": kind}
	for k, v := range fields {
		rec[k] = v
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	fmt.Fprintf(e.w, "%s\n", data)
	if f, ok := e.w.(interface{ Sync() error }); ok {
		_ = f.Sync()
	}
}

// Handle relays an event through this emitter, assigning its wire sequence.
func (e *Emitter) Handle(event Event) {
	e.Emit(event.Name, event.Fields)
}
