package workflow

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
	if model.editor.Path != "" || model.editor.Source() != before || host.dialog != nil {
		t.Fatal("late work changed a closed screen or opened a dialog")
	}
}
