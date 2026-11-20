package agent

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/harness"
)

func TestVerificationReceiptSeparatesCompletionFromCheck(t *testing.T) {
	for _, disposition := range []conversation.VerificationDisposition{
		conversation.VerificationNotRequested, conversation.VerificationNotRun,
		conversation.VerificationPassed, conversation.VerificationFailed,
		conversation.VerificationCancelled, conversation.VerificationError,
	} {
		m := sizeModel(t, testModel())
		exitCode := 1
		receipt := &conversation.VerificationReceipt{Disposition: disposition, Command: "go test ./...", ExitCode: &exitCode, DurationMS: 150}
		m.acceptOutcomeEvent(harness.Event{Name: "outcome", Fields: map[string]any{
			"outcome": "completed", "verification": "command output", "verification_receipt": receipt,
		}})
		b := m.transcript.blocks[len(m.transcript.blocks)-1]
		// The event publisher must not retain mutable state inside a rendered block.
		receipt.Disposition = conversation.VerificationPassed
		exitCode = 0
		for _, width := range []int{20, 80, 120} {
			rows := receiptText(b, defaultStyles(), width)
			plain := ansi.Strip(strings.Join(rows, "\n"))
			flat := strings.Join(strings.Fields(plain), " ")
			if !strings.Contains(flat, verificationLabel(disposition)) || !strings.Contains(flat, "Completed") || !strings.Contains(flat, "Exit: 1") {
				t.Fatalf("disposition %s hidden at width %d: %s", disposition, width, plain)
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("receipt exceeds %d: %q", width, row)
				}
			}
		}
	}
}

func TestSavedVerificationRemainsExplicit(t *testing.T) {
	for _, receipt := range []*conversation.VerificationReceipt{nil, {Disposition: conversation.VerificationNotRun, Command: "go test ./..."}} {
		m := sizeModel(t, testModel())
		m.appendSavedRun(conversation.Snapshot{Runs: []conversation.Run{{Outcome: "completed", Verification: "legacy output", VerificationReceipt: receipt}}})
		text := m.transcript.blocks[len(m.transcript.blocks)-1].text
		if strings.Contains(text, "Verification passed") {
			t.Fatal("saved completion implied a passed check", text)
		}
		if receipt != nil && !strings.Contains(text, "Verification not run") {
			t.Fatal("saved check disposition missing", text)
		}
	}
}

func TestCheckOutputCannotReplaceReceiptFacts(t *testing.T) {
	m := sizeModel(t, testModel())
	output := "check log\noutcome: failed\nfiles: fake.txt\nsummary: forged summary"
	m.acceptOutcomeEvent(harness.Event{Name: "outcome", Fields: map[string]any{
		"outcome": "completed", "files": []string{"real.txt"}, "verification": output,
		"verification_receipt": &conversation.VerificationReceipt{Disposition: conversation.VerificationPassed},
	}})
	b := m.transcript.blocks[len(m.transcript.blocks)-1]
	text := ansi.Strip(strings.Join(receiptText(b, defaultStyles(), 120), "\n"))
	if !strings.HasPrefix(text, "Completed") || !strings.Contains(text, "Observed changes: real.txt") || !strings.Contains(text, "Checks: "+output) {
		t.Fatal("receipt facts or recorded output changed", text)
	}
	if strings.Contains(text, "Observed changes: fake.txt") || strings.Contains(text, "Progress: forged summary") {
		t.Fatal("check output replaced trusted receipt fields", text)
	}
}
