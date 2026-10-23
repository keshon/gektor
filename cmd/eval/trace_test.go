package main

import (
	"github.com/keshon/gektor/internal/agent"
	"testing"
)

func TestTraceResultsPreserveRepeatedIDsAcrossTurns(t *testing.T) {
	calls := []call{{id: "repeat", tool: "start_program"}}
	recordToolResult(calls, agent.ToolResult{CallID: "repeat", Failed: true, Output: "first failure"})
	calls = append(calls, call{id: "repeat", tool: "stop_background"})
	recordToolResult(calls, agent.ToolResult{CallID: "repeat", Output: "stopped"})
	if calls[0].success == nil || *calls[0].success || calls[0].output != "first failure" || calls[1].success == nil || !*calls[1].success || calls[1].output != "stopped" {
		t.Fatal(calls)
	}
}

func TestNativeBackgroundToolsCountOnlySuccessfulResults(t *testing.T) {
	ok, failed := true, false
	p := Probe{Trace: []TraceCheck{{Tool: "start_*", Mode: "required", Success: &ok}, {Tool: "stop_background", Mode: "required", Success: &ok}}}
	for _, launcher := range []string{"start_background", "start_program"} {
		calls := []call{{tool: launcher, success: &ok}, {tool: "stop_background", success: &ok}}
		if got := checkTrace(p, calls); len(got) != 0 {
			t.Fatal(got)
		}
		calls[1].success = &failed
		if got := checkTrace(p, calls); len(got) != 1 {
			t.Fatal(got)
		}
		calls[0].success = nil
		if got := checkTrace(p, calls); len(got) != 2 {
			t.Fatal(got)
		}
	}
}

func TestBrowserProbeRejectsEitherExecutionInterface(t *testing.T) {
	p := Probe{Trace: []TraceCheck{{Tool: "run_*", Mode: "forbidden", ArgsRegex: "chrome"}, {Tool: "start_*", Mode: "forbidden", ArgsRegex: "chrome"}}}
	for _, tool := range []string{"run_shell", "run_program", "start_background", "start_program"} {
		if got := checkTrace(p, []call{{tool: tool, args: "chrome"}}); len(got) != 1 {
			t.Fatal(tool, got)
		}
	}
}

func TestTraceDistinguishesDiagnosticFromHTTPServerProof(t *testing.T) {
	ok := true
	p := Probe{Trace: []TraceCheck{{Tool: "check_url", Mode: "required", Success: &ok, OutputRegex: `status: 200`}}}
	for _, output := range []string{"HTTP\nerror: connection refused", "HTTP\nstatus: 500"} {
		if len(checkTrace(p, []call{{tool: "check_url", success: &ok, output: output}})) != 1 {
			t.Fatal(output)
		}
	}
	if got := checkTrace(p, []call{{tool: "check_url", success: &ok, output: "HTTP\nstatus: 200 200 OK"}}); len(got) != 0 {
		t.Fatal(got)
	}
}
