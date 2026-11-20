package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/keshon/gektor/internal/conversation"
)

func TestVerificationReceiptReachesOutcomeEvent(t *testing.T) {
	var outcome Event
	session, err := New(Config{
		Env: testEnv(t, stubOK{}, nil), Task: "task", VerifyCommand: "test command",
		Verify: func(context.Context) (string, error) { return "legacy output", nil },
		Events: eventVisitor(func(event Event) {
			if event.Name == "outcome" {
				outcome = event
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	receipt, ok := outcome.Fields["verification_receipt"].(*conversation.VerificationReceipt)
	if !ok || receipt.Disposition != conversation.VerificationPassed || receipt.Command != "test command" || receipt.ExitCode == nil || *receipt.ExitCode != 0 || outcome.Fields["verification"] != "legacy output" {
		t.Fatalf("outcome=%+v", outcome.Fields)
	}
	wire, err := json.Marshal(outcome.Fields)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Receipt conversation.VerificationReceipt `json:"verification_receipt"`
		Legacy  string                           `json:"verification"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Receipt.Disposition != conversation.VerificationPassed || decoded.Receipt.Command != "test command" || decoded.Legacy != "legacy output" {
		t.Fatalf("wire=%s", wire)
	}
}

func TestOutcomeObserverCannotMutateSessionReport(t *testing.T) {
	session, err := New(Config{
		Env: testEnv(t, stubOK{}, nil), Task: "task", VerifyCommand: "test command",
		Verify: func(context.Context) (string, error) { return "checked", nil },
		Events: eventVisitor(func(event Event) {
			if event.Name == "outcome" {
				receipt := event.Fields["verification_receipt"].(*conversation.VerificationReceipt)
				receipt.Disposition = conversation.VerificationFailed
				*receipt.ExitCode = 7
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(t.Context()); err != nil {
		t.Fatal(err)
	}
	report := session.Report()
	if report.VerificationReceipt.Disposition != conversation.VerificationPassed || *report.VerificationReceipt.ExitCode != 0 {
		t.Fatalf("observer mutated session report: %+v", report.VerificationReceipt)
	}
	*report.VerificationReceipt.ExitCode = 3
	if *session.Report().VerificationReceipt.ExitCode != 0 {
		t.Fatal("report caller mutated session report")
	}
}
