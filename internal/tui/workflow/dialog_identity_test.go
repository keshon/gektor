package workflow

import (
	"testing"

	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestReplacedFormRejectsLateSubmissionAndClose(t *testing.T) {
	host := workflowTestModel(t)
	m := host.workflows
	oldCalls, currentCalls := 0, 0
	m.openForm(screen.Form{Title: "Old"}, func(map[string]string) error { oldCalls++; return nil })
	old := host.chrome.DialogID()
	m.openForm(screen.Form{Title: "Current"}, func(map[string]string) error { currentCalls++; return nil })
	current := host.chrome.DialogID()
	m.HandleControl(screen.Event{Kind: screen.Apply, ID: old})
	m.HandleControl(screen.Event{Kind: screen.Closed, ID: old})
	if oldCalls != 0 || currentCalls != 0 || host.chrome.DialogID() != current || m.form.dialog != current {
		t.Fatal("late form event affected the replacement")
	}
	m.HandleControl(screen.Event{Kind: screen.Apply, ID: current})
	if currentCalls != 1 || host.chrome.DialogID() != (screen.DialogID{}) {
		t.Fatal("current form submission did not apply and close")
	}
}

func TestReplacedPermissionDeniesOnlyItsOwnRequest(t *testing.T) {
	host := workflowTestModel(t)
	m := host.workflows
	oldReply := make(chan permission.Effect, 1)
	currentReply := make(chan permission.Effect, 1)
	m.acceptPermission(workflowPermission{tool: "write_file", resource: "old.txt", reply: oldReply})
	old := host.chrome.DialogID()
	m.acceptPermission(workflowPermission{tool: "write_file", resource: "current.txt", reply: currentReply})
	current := host.chrome.DialogID()
	host.syncControls()
	if effect := <-oldReply; effect != permission.Deny {
		t.Fatal("replacing permission did not deny its request")
	}
	m.HandleControl(screen.Event{Kind: screen.Apply, ID: old, Values: map[string]string{"decision": "Allow for this session"}})
	select {
	case <-currentReply:
		t.Fatal("late permission submission answered the replacement")
	default:
	}
	if m.permissionDialog != current || m.approvals.Allows("write_file", "old.txt") {
		t.Fatal("late permission submission changed permission ownership or approvals")
	}
	host.closeDialog()
	if effect := <-currentReply; effect != permission.Deny {
		t.Fatal("closing permission did not deny its request")
	}
	if len(m.permissionRequests) != 0 {
		t.Fatal("closed permissions retained reply channels")
	}
}

func TestReplacedProposalRejectsLateApply(t *testing.T) {
	host := workflowTestModel(t)
	m := host.workflows
	m.editor.Replace(workflowRecipe())
	before := m.editor.Source()
	host.chrome.PresentDialog(screen.Dialog{Title: "Proposal"})
	old := host.chrome.DialogID()
	m.proposalDialog = old
	m.proposalBefore = before
	m.proposalDraft = m.editor.Clone()
	m.proposalDraft.Name = "Must not apply"
	host.chrome.PresentDialog(screen.Dialog{Title: "Replacement"})
	current := host.chrome.DialogID()
	m.HandleControl(screen.Event{Kind: screen.DialogActionRequested, ID: old, Action: "apply-proposal"})
	if m.editor.Source() != before || host.chrome.DialogID() != current {
		t.Fatal("late proposal event changed the draft or replacement dialog")
	}
}
