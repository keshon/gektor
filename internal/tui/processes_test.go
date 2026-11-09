package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/tools"
	"github.com/keshon/gektor/internal/workspace"
)

func TestProcessManagerChild(t *testing.T) {
	if os.Getenv("GEKTOR_UI_PROCESS_HELPER") != "1" {
		return
	}
	fmt.Print("managed output marker\n")
	time.Sleep(time.Minute)
	os.Exit(0)
}

func processModel(t *testing.T) (*model, string) {
	t.Helper()
	t.Setenv("GEKTOR_UI_PROCESS_HELPER", "1")
	ws, err := workspace.NewWithSkillRoots(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	p := tools.NewBackgroundProcesses()
	t.Cleanup(p.StopAll)
	args, _ := json.Marshal(map[string]any{"program": os.Args[0], "args": []string{"-test.run=^TestProcessManagerChild$"}})
	out, err := (tools.StartProgram{WS: ws, Procs: p, SettleTime: 100 * time.Millisecond}).Run(context.Background(), args)
	if err != nil {
		t.Fatal(out, err)
	}
	id := strings.TrimPrefix(strings.Split(out, "\n")[0], "id: ")
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, captured, err := p.Inspect(id)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(captured, "managed output marker") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child output never appeared", captured)
		}
		time.Sleep(20 * time.Millisecond)
	}
	m := sizeModel(t, testModel())
	m.state = stDone
	m.baseEnv.Procs = p
	return m, id
}

func TestProcessManagerConfirmationAndAsynchronousStop(t *testing.T) {
	m, id := processModel(t)
	m.command("/processes")
	view := m.dialog.table.model
	if view.Selected() != id {
		t.Fatal(view.Rows)
	}
	m.confirmStopProcess(id, view)
	if !m.dialog.form.Destructive || m.dialog.form.Index != 1 {
		t.Fatal("stop confirmation must default to Cancel")
	}
	m.formKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p, _, _ := m.baseEnv.Procs.Inspect(id); p.Exited {
		t.Fatal("Cancel stopped process")
	}
	m.pendingCommands = nil
	m.openProcesses(view)
	m.confirmStopProcess(id, view)
	m.focusForm(0)
	m.applyForm()
	if m.dialog != nil {
		t.Fatal("confirmation did not close")
	}
	msg := m.pendingCommands[0]().(processStopMsg)
	m.pendingCommands = nil
	cmd := m.stopProcess(msg)
	if cmd == nil || m.dialog == nil {
		t.Fatal("stop did not start asynchronously")
	}
	result := cmd().(processStoppedMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(result)
	if p, _, _ := m.baseEnv.Procs.Inspect(id); !p.Exited || !p.Stopped {
		t.Fatal(p)
	}
	if m.dialog.table == nil || processStatus(m.baseEnv.Procs.List()[0]) != "Stopped" {
		t.Fatal("manager did not return after stop")
	}
	for _, a := range m.dialog.table.actions() {
		if a.ID == "stop" && a.Enabled {
			t.Fatal("stopped process remains stoppable")
		}
	}
}

func TestProcessManagerOutputReturnAndLateCompletion(t *testing.T) {
	m, id := processModel(t)
	m.openProcesses(nil)
	view := m.dialog.table.model
	m.inspectProcess(id, view)
	if text := strings.Join(m.dialog.lines, "\n"); !strings.Contains(text, "Workspace:") || !strings.Contains(text, "managed output marker") {
		t.Fatal(m.dialog.lines)
	}
	m.closeDialog()
	m.Update(processListMsg{view})
	if m.dialog.table == nil || m.dialog.table.model.Selected() != id {
		t.Fatal("return lost process selection")
	}
	m.dialog = nil
	cmd := m.stopProcess(processStopMsg{id, view})
	m.closeDialog()
	m.openDialog("Another dialog", []string{"keep this"})
	other := m.dialog
	m.Update(cmd())
	if m.dialog != other {
		t.Fatal("late stop completion stole focus")
	}
}

func TestProcessManagerEmptyAndMenus(t *testing.T) {
	for _, workflow := range []bool{false, true} {
		m := sizeModel(t, testModel())
		m.state = stDone
		if workflow {
			m = workflowTestModel(t)
		}
		found := false
		for _, item := range m.menuItems("execution") {
			if item.command == "/processes" && item.enabled {
				found = true
			}
		}
		if !found {
			t.Fatal("process manager missing from Run menu", workflow)
		}
		m.openProcesses(nil)
		if len(m.dialog.table.model.Rows) != 0 {
			t.Fatal("showed foreign process")
		}
		for _, width := range []int{40, 80, 120} {
			sizeModelTo(t, m, width, 24)
			if m.dialogView() == "" {
				t.Fatal("empty modal")
			}
		}
	}
}
