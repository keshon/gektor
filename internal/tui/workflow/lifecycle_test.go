package workflow

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workflow/store"
)

func TestClosedScreenReleasesWaitAndRejectsLateWork(t *testing.T) {
	host := workflowTestModel(t)
	model := host.workflows
	wait := model.wait()
	returned := make(chan tea.Msg, 1)
	go func() { returned <- wait() }()
	model.Close()
	select {
	case msg := <-returned:
		if msg != nil {
			t.Fatal("closed screen returned an event", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("screen cleanup left an event waiter blocked")
	}
	before := model.editor.Source()
	model.Update(workflowResult{action: "open-path", source: before, path: "late.json"})
	model.Command("add")
	if model.editor.Path != "" || model.editor.Source() != before || host.chrome.Dialog != nil {
		t.Fatal("late work changed a closed screen or opened a dialog")
	}
}

func TestWorkflowScreenCloseCancelsItsOperation(t *testing.T) {
	m := workflowTestModel(t)
	w := m.workflows
	operation, cancel := context.WithCancel(w.ctx)
	w.cancel = cancel
	w.Close()
	if operation.Err() != context.Canceled || w.ctx.Err() != context.Canceled {
		t.Fatal("closing the screen left its operation or context active")
	}
	if m.base.Err() != nil {
		t.Fatal("closing the workflow screen canceled the root context")
	}
}

func TestWorkflowClosePersistsCancelledRunBeforeReturning(t *testing.T) {
	host := workflowTestModel(t)
	model := host.workflows
	model.editor.Definition = workflowRecipe()
	model.editor.Definition.Steps[0].Args = json.RawMessage(`{"code":"for (;;) {}","input":null}`)
	model.editor.Path = "recipe.json"
	model.action(workflowAction{"run-inputs", "{}"})

	var runID string
	deadline := time.After(5 * time.Second)
	for runID == "" {
		select {
		case event := <-model.events:
			switch message := event.(type) {
			case workflowRunID:
				runID = string(message)
			case workflowPermission:
				t.Fatal("unexpected permission request")
			case workflowResult:
				t.Fatalf("run ended before cancellation: %v", message.err)
			}
		case <-deadline:
			t.Fatal("workflow did not start")
		}
	}

	db, err := store.OpenStore(userdirs.Sub("workflows"), host.wsRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// OnRun precedes the first save. Wait until execution has a persisted record.
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, readErr := db.Run(runID)
		if readErr == nil && run.Status == "running" {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("run was not persisted: %v", readErr)
		}
	}

	model.Close()
	run, err := db.Run(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" || run.Record.Steps[0].Status != "cancelled" {
		t.Fatalf("Close returned before cancellation was persisted: %+v", run)
	}
	if run.Record.Finished == nil {
		t.Fatal("cancelled run has no finish time")
	}
	model.Close()
	if model.Command("run") != nil {
		t.Fatal("closed screen accepted new work")
	}
}
