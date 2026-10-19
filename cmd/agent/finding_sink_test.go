package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/checks"
	"github.com/keshon/tars/internal/events"
)

// json mode used to return a nil sink, and a nil sink is not a quiet
// sink: tools.Checked treats a nil report as "scanning disabled" and
// returns the tool unwrapped. Direct `-mode json` runs never reach a
// session emitter, so findings were emitted by nobody at all.
func TestFindingSink_JSONModeIsNotNil(t *testing.T) {
	var buf bytes.Buffer
	sink := findingSink(events.New(&buf), "json")
	if sink == nil {
		t.Fatal("json mode must deliver findings, not disable them")
	}
	sink(checks.SessionEnd, checks.Finding{Rule: "gofmt", Path: "a.go", Line: 3, Summary: "not gofmt-clean"})

	out := buf.String()
	if !strings.Contains(out, `"event":"finding"`) || !strings.Contains(out, `"scope":"session-end"`) {
		t.Fatalf("json mode emitted %q, want a session-end finding event", out)
	}
}

// A json run with no emitter must fall back to printing rather than
// returning nil, which would silently switch scanning off again.
func TestFindingSink_JSONWithoutEmitterStillDelivers(t *testing.T) {
	if findingSink(nil, "json") == nil {
		t.Fatal("a nil sink disables scanning; fall back to print instead")
	}
}
