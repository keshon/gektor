package tools

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestBackgroundManagerSnapshotAndConcurrentStop(t *testing.T) {
	procs := NewBackgroundProcesses()
	defer procs.StopAll()
	owner := filepath.Join(t.TempDir(), "session-one", "state.json")
	store := NewOutputStore(owner)
	cmd := shellCommand(t.Context(), longRunningCommand)
	cmd.Dir = t.TempDir()
	id, _ := procs.start(store.Context(t.Context()), cmd)
	rows := procs.List()
	if len(rows) != 1 || rows[0].ID != id || rows[0].PID <= 0 || rows[0].Session != owner || rows[0].Directory != cmd.Dir || rows[0].Started.IsZero() || rows[0].Exited {
		t.Fatal(rows)
	}
	rows[0].Args[0] = "changed"
	if procs.List()[0].Args[0] == "changed" {
		t.Fatal("caller changed owned process arguments")
	}
	procs.mu.Lock()
	procs.procs[id].output.Write([]byte("captured marker\n"))
	procs.mu.Unlock()
	for range 2 {
		if _, out, err := procs.Inspect(id); err != nil || !strings.Contains(out, "captured marker") {
			t.Fatal(out, err)
		}
	}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := procs.Stop(id); err != nil {
				t.Error(err)
			}
		})
	}
	for range 20 {
		procs.List()
		procs.Inspect(id)
	}
	wg.Wait()
	info, _, err := procs.Inspect(id)
	if err != nil || !info.Exited || !info.Stopped {
		t.Fatal(info, err)
	}
	if _, _, err := procs.Inspect("1234"); err == nil {
		t.Fatal("accepted arbitrary PID")
	}
	if err := procs.Stop("not-owned"); err == nil {
		t.Fatal("accepted unmanaged ID")
	}
	if notes := procs.Notifications(store.Context(t.Context())); len(notes) != 1 {
		t.Fatal("inspection consumed session notifications", notes)
	}
}

func TestBackgroundManagerListsFailedStart(t *testing.T) {
	procs := NewBackgroundProcesses()
	cmd := exec.Command(filepath.Join(t.TempDir(), "does-not-exist.exe"))
	id, _ := procs.start(t.Context(), cmd)
	info, _, err := procs.Inspect(id)
	if err != nil || !info.Exited || info.Error == "" || info.PID != 0 {
		t.Fatal(info, err)
	}
	// Listing must not pull unrelated OS processes into a new registry.
	if got := NewBackgroundProcesses().List(); len(got) != 0 {
		t.Fatal(got)
	}
}
