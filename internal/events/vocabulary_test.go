package events

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEmitFinding_Fields(t *testing.T) {
	recorder := &recordedEvents{}
	e := NewSink(recorder)
	EmitFinding(e, "per-edit", "secret-aws", "a.go", 3, "possible secret (secret-aws)")
	got := recorder.events
	if len(got) != 1 || got[0].Name != "finding" {
		t.Fatalf("events = %+v", got)
	}
	fields := got[0].Fields
	if fields["scope"] != "per-edit" || fields["rule"] != "secret-aws" || fields["line"] != 3 {
		t.Fatalf("fields = %v", fields)
	}
	EmitFinding(nil, "x", "y", "z", 0, "w") // nil-safe
}

func TestEmitNudge_Fields(t *testing.T) {
	recorder := &recordedEvents{}
	e := NewSink(recorder)
	EmitNudge(e, "verify", "[harness] check your work")
	got := recorder.events
	if len(got) != 1 || got[0].Name != "nudge" {
		t.Fatalf("events = %+v", got)
	}
	if got[0].Fields["kind"] != "verify" {
		t.Fatalf("fields = %v", got[0].Fields)
	}
	EmitNudge(nil, "x", "y") // nil-safe
}

func TestEmitDelta_Fields(t *testing.T) {
	recorder := &recordedEvents{}
	e := NewSink(recorder)
	EmitDelta(e, "hel")
	got := recorder.events
	if len(got) != 1 || got[0].Name != "delta" {
		t.Fatalf("events = %+v", got)
	}
	if got[0].Fields["text"] != "hel" {
		t.Fatalf("fields = %v", got[0].Fields)
	}
	EmitDelta(nil, "x") // nil-safe
}

func TestSinkAndWireShareEventProjection(t *testing.T) {
	recorder := &recordedEvents{}
	emitter := NewSink(recorder)
	fields := map[string]any{"processed": 1024, "paths": []string{"a"}}
	emitter.Emit("progress", fields)
	ev := recorder.events[0]
	fields["processed"] = 2048
	if ev.Fields["processed"] != 1024 {
		t.Fatal("sink changed native values or retained mutable field map")
	}
	var wire bytes.Buffer
	wireEmitter := New(&wire)
	wireEmitter.Emit(ev.Name, ev.Fields)
	var got map[string]any
	if err := json.Unmarshal(wire.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["event"] != ev.Name || got["seq"] != float64(ev.Seq) || got["processed"] != float64(1024) {
		t.Fatal(got)
	}
}
