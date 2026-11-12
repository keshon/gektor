package agent

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/roles"
	toolbackground "github.com/keshon/gektor/internal/tools/background"
	"github.com/keshon/gektor/internal/workspace"
)

func TestCommand_HelpAndUnknown(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("/help")
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
	updated, _ = updated.(*Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := updated.(*Model)
	if mm.quit || mm.state != stDone {
		t.Fatalf("help must not quit or resume: quit=%v state=%v", mm.quit, mm.state)
	}
	if mm.chrome.Dialog == nil || !mm.chrome.Dialog.Help {
		t.Fatal("/help must open the shared help modal")
	}
	mm.chrome.Dialog.Page = 3
	for _, want := range []string{"/help", "/sessions"} {
		if !strings.Contains(mm.testControls().DialogView(), want) {
			t.Fatalf("help missing %q", want)
		}
	}
	// A second unrelated command works on its own model state.
	mm2 := sizeModel(t, testModel())
	mm2.state = stDone
	mm2.input.SetValue("/nope")
	updated, _ = mm2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mm := updated.(*Model); mm.quit || mm.state != stDone {
		t.Fatal("unknown command must not quit or resume")
	}
}

func TestCommand_Quit(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("/quit")
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyTab})
	updated, _ = updated.(*Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if mm := updated.(*Model); len(mm.TakeRequests()) != 1 {
		t.Fatal("/quit must quit")
	}
}

func TestCommand_NewStartsFresh(t *testing.T) {
	m := sizeModel(t, testModel())
	ws, err := workspace.New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	m.newSession = func(task, stateFile string, images []string) (*harness.Session, error) {
		if task != "second" {
			t.Errorf("task = %q", task)
		}
		if stateFile == "" || !strings.HasSuffix(stateFile, "state.json") {
			t.Errorf("stateFile = %q", stateFile)
		}
		return harness.New(harness.Config{
			Task: task,
			Env: roles.Env{
				Client: stubDone{},
				WS:     ws,
				Procs:  toolbackground.New(),
			},
		})
	}
	var sent int32
	m.send = func(tea.Msg) { atomic.AddInt32(&sent, 1) }
	m.state = stDone
	m.input.SetValue("/new second")
	updated, _ := m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm := updated.(*Model)
	if mm.state != stRunning {
		t.Fatalf("state = %v, want running", mm.state)
	}
	if len(mm.transcript.blocks) != 2 || mm.transcript.blocks[0].role != roleStatus || mm.transcript.blocks[1].role != roleUser {
		t.Fatalf("transcript must open marker + task echo: %+v", mm.transcript.blocks)
	}
	if mm.transcript.blocks[1].text != "second" {
		t.Fatalf("echo = %q, want verbatim task", mm.transcript.blocks[1].text)
	}
	deadline := time.Now().Add(10 * time.Second)
	for atomic.LoadInt32(&sent) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&sent) == 0 {
		t.Fatal("fresh run never reported completion")
	}
}
