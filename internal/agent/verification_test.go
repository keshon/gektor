package agent

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

func TestVerificationExitProcess(t *testing.T) {
	if os.Getenv("GEKTOR_VERIFICATION_EXIT_TEST") == "1" {
		os.Exit(7)
	}
}

func TestVerificationReceiptRetainsMeasuredResult(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestVerificationExitProcess$")
	command.Env = append(os.Environ(), "GEKTOR_VERIFICATION_EXIT_TEST=1")
	exitErr := command.Run()
	var exitError *exec.ExitError
	if !errors.As(exitErr, &exitError) || exitError.ExitCode() != 7 {
		t.Fatalf("test process did not exit 7: %v", exitErr)
	}

	tests := []struct {
		name        string
		command     string
		err         error
		cancel      bool
		disposition conversation.VerificationDisposition
		exitCode    *int
	}{
		{name: "command passed", command: "test command", disposition: conversation.VerificationPassed, exitCode: new(0)},
		{name: "validator passed", disposition: conversation.VerificationPassed},
		{name: "command failed", command: "test command", err: exitErr, disposition: conversation.VerificationFailed, exitCode: new(7)},
		{name: "validator rejected", err: errors.New("recipe did not validate"), disposition: conversation.VerificationFailed},
		{name: "command could not start", command: "test command", err: errors.New("executable missing"), disposition: conversation.VerificationError},
		{name: "check deadline", command: "test command", err: context.DeadlineExceeded, disposition: conversation.VerificationCancelled},
		{name: "cancelled callback reports success", command: "test command", cancel: true, disposition: conversation.VerificationCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			client := &stubClient{responses: []llm.ChatResponse{say("done")}}
			state := filepath.Join(t.TempDir(), "state.json")
			calls := 0
			a := New(Config{
				Client: client, Tools: NewRegistry(), StateFile: state, VerifyCommand: tt.command,
				Verify: func(context.Context) (string, error) {
					calls++
					if tt.cancel {
						cancel()
					}
					return strings.Repeat("output", 3000), tt.err
				},
			})
			answer, err := a.Run(ctx, "task")
			report := a.Report()
			receipt := report.VerificationReceipt
			if receipt == nil || receipt.Disposition != tt.disposition || receipt.Command != tt.command || !reflect.DeepEqual(receipt.ExitCode, tt.exitCode) {
				t.Fatalf("receipt=%+v", receipt)
			}
			if receipt.DurationMS < 0 || receipt.Output == "" || len(receipt.Output) > 8192 || receipt.Output != report.Verification {
				t.Fatalf("invalid measured output: %+v", receipt)
			}
			passed := tt.disposition == conversation.VerificationPassed
			if passed != (err == nil) || passed != (answer == "done") || passed != (report.Final == "done") || passed != (report.Outcome == "completed") {
				t.Fatalf("false completion: answer=%q err=%v report=%+v", answer, err, report)
			}
			if passed != (receipt.Error == "") || calls != 1 || client.calls != 1 {
				t.Fatalf("verification repeated or lost error: calls=%d model=%d receipt=%+v", calls, client.calls, receipt)
			}
			saved, loadErr := conversation.Load(state)
			if loadErr != nil || len(saved.Runs) != 1 {
				t.Fatalf("load run: %+v %v", saved.Runs, loadErr)
			}
			if !reflect.DeepEqual(saved.Runs[0].VerificationReceipt, receipt) || saved.Runs[0].Verification != report.Verification {
				t.Fatalf("saved receipt differs: %+v", saved.Runs[0])
			}
		})
	}
}

func TestVerificationReceiptDoesNotInventACheck(t *testing.T) {
	t.Run("not requested", func(t *testing.T) {
		a := New(Config{Client: &stubClient{responses: []llm.ChatResponse{say("done")}}, Tools: NewRegistry()})
		_, err := a.Run(t.Context(), "task")
		if err != nil || a.Report().VerificationReceipt.Disposition != conversation.VerificationNotRequested {
			t.Fatalf("report=%+v err=%v", a.Report(), err)
		}
	})
	t.Run("run stopped first", func(t *testing.T) {
		a := New(Config{
			Client: &repeatingToolClient{args: []byte(`{}`)}, Tools: NewRegistry(mutStub{"write_file"}), MaxSteps: 1,
			VerifyCommand: "test command",
			Verify:        func(context.Context) (string, error) { t.Fatal("check ran after step limit"); return "", nil },
		})
		_, err := a.Run(t.Context(), "task")
		receipt := a.Report().VerificationReceipt
		if !errors.Is(err, ErrMaxSteps) || receipt.Disposition != conversation.VerificationNotRun || receipt.Command != "test command" || receipt.ExitCode != nil || receipt.Output != "" {
			t.Fatalf("receipt=%+v err=%v", receipt, err)
		}
	})
}

func TestReportMutationCannotRewriteSavedEvidence(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	a := New(Config{
		Client: &stubClient{responses: []llm.ChatResponse{say("done")}}, Tools: NewRegistry(), StateFile: state,
		VerifyCommand: "test command", Verify: func(context.Context) (string, error) { return "checked", nil },
	})
	if _, err := a.Run(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	a.report.RecentTools = []ToolEvidence{{Name: "read_file"}}
	a.report.Warnings = []string{"warning"}
	a.report.MutatedPaths = []string{"file.go"}
	a.report.OpenTodos = []string{"task"}
	a.report.Prompt.Sections = []llm.ContextCost{{Name: "system"}}
	a.report.Prompt.Tools = []llm.ContextCost{{Name: "read_file"}}
	a.report.Prompt.Notes = []string{"estimate"}
	want := a.Report()
	report := a.Report()
	report.VerificationReceipt.Disposition = conversation.VerificationFailed
	report.VerificationReceipt.Output = "rewritten"
	*report.VerificationReceipt.ExitCode = 7
	report.RecentTools[0].Name = "rewritten"
	report.Warnings[0] = "rewritten"
	report.MutatedPaths[0] = "rewritten"
	report.OpenTodos[0] = "rewritten"
	report.Prompt.Sections[0].Name = "rewritten"
	report.Prompt.Tools[0].Name = "rewritten"
	report.Prompt.Notes[0] = "rewritten"
	a.MutatedPaths()[0] = "rewritten"
	if !reflect.DeepEqual(a.Report(), want) {
		t.Fatal("caller changed live report evidence")
	}
	// A later checkpoint must not persist mutations made through a returned report.
	if err := a.snapshot.Save(state); err != nil {
		t.Fatal(err)
	}
	saved, err := conversation.Load(state)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Runs[0].VerificationReceipt, want.VerificationReceipt) {
		t.Fatalf("caller rewrote saved evidence: %+v", saved.Runs[0])
	}
}
