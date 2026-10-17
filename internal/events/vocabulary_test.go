package events

import (
	"bytes"
	"testing"

)
func TestEmitFinding_RoundTrips(t *testing.T) {
	var buf bytes.Buffer
	EmitFinding(New(&buf), "per-edit", "gofmt", "a.go", 3, "not gofmt-clean")
	var got []Event
	w := &callbackWriter{onEvent: func(ev Event) { got = append(got, ev) }}
	if _, err := w.Write(buf.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(got) != 1 || got[0].Name != "finding" {
		t.Fatalf("events = %+v", got)
	}
	fields := got[0].Fields
	if fields["scope"] != "per-edit" || fields["rule"] != "gofmt" || fields["line"] != float64(3) {
		t.Fatalf("fields = %v", fields)
	}
	EmitFinding(nil, "x", "y", "z", 0, "w") // nil-safe
}

func TestEmitNudge_RoundTrips(t *testing.T) {
	var buf bytes.Buffer
	EmitNudge(New(&buf), "verify", "[harness] check your work")
	var got []Event
	w := &callbackWriter{onEvent: func(ev Event) { got = append(got, ev) }}
	if _, err := w.Write(buf.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(got) != 1 || got[0].Name != "nudge" {
		t.Fatalf("events = %+v", got)
	}
	if got[0].Fields["kind"] != "verify" {
		t.Fatalf("fields = %v", got[0].Fields)
	}
	EmitNudge(nil, "x", "y") // nil-safe
}

func TestEmitDelta_RoundTrips(t *testing.T) {
	var buf bytes.Buffer
	EmitDelta(New(&buf), "hel")
	var got []Event
	w := &callbackWriter{onEvent: func(ev Event) { got = append(got, ev) }}
	if _, err := w.Write(buf.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(got) != 1 || got[0].Name != "delta" {
		t.Fatalf("events = %+v", got)
	}
	if got[0].Fields["text"] != "hel" {
		t.Fatalf("fields = %v", got[0].Fields)
	}
	EmitDelta(nil, "x") // nil-safe
}

