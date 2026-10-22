package tools

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestBackgroundNotificationHelper(t *testing.T) {
	if os.Getenv("GEKTOR_BACKGROUND_HELPER") != "1" {
		return
	}
	fmt.Print("measured background result")
	os.Exit(0)
}

func TestBackgroundCompletionWaitsForOwningSession(t *testing.T) {
	store := &OutputStore{dir: t.TempDir()}
	ctx := store.Context(t.Context())
	procs := NewBackgroundProcesses()
	defer procs.StopAll()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBackgroundNotificationHelper$")
	cmd.Env = append(os.Environ(), "GEKTOR_BACKGROUND_HELPER=1")
	id, _ := procs.start(ctx, cmd)
	procs.mu.Lock()
	done := procs.procs[id].done
	procs.mu.Unlock()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("helper never exited")
	}
	other := (&OutputStore{dir: t.TempDir()}).Context(t.Context())
	if notes := procs.Notifications(other); len(notes) != 0 {
		t.Fatal("cross-session notification", notes)
	}
	notes := procs.Notifications(ctx)
	if len(notes) != 1 || !strings.Contains(notes[0], "measured background result") {
		t.Fatal(notes)
	}
	if len(procs.Notifications(ctx)) != 0 {
		t.Fatal("completion delivered twice")
	}
}
