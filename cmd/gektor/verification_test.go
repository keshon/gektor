package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
)

func TestCLIReportNamesVerificationWithoutCallingItTaskSuccess(t *testing.T) {
	var output strings.Builder
	reportAgentRun(agent.RunReport{
		Outcome: "failed", Verification: "diagnostics",
		VerificationReceipt: &conversation.VerificationReceipt{
			Disposition: conversation.VerificationFailed, Command: "go test ./...", ExitCode: new(1), DurationMS: 42, Error: "exit status 1",
		},
	}, "state.json", resolvedLaunch{}, func(format string, args ...any) { fmt.Fprintf(&output, format+"\n", args...) })
	for _, fact := range []string{"outcome: failed", "verification: failed; duration: 42ms", "verification command: go test ./...", "verification exit code: 1", "verification error: exit status 1", "checks: diagnostics"} {
		if !strings.Contains(output.String(), fact) {
			t.Fatalf("missing %q in %s", fact, output.String())
		}
	}
}
