package background

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tooloutput "github.com/keshon/gektor/internal/tools/output"
)

func TestBackgroundNotificationHelper(t *testing.T) {
	if os.Getenv("GEKTOR_BACKGROUND_HELPER") != "1" {
		return
	}
	fmt.Print(strings.Repeat("measured background result\n", 1000))
	if os.Getenv("GEKTOR_BACKGROUND_FAILURE") == "1" {
		os.Exit(7)
	}
	os.Exit(0)
}

func TestBackgroundCompletionWaitsForOwningSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	for _, outcome := range []string{"completed", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			store := tooloutput.New(t.TempDir())
			ctx := store.Context(t.Context())
			procs := New()
			defer procs.StopAll()
			cmd := exec.Command(os.Args[0], "-test.run=^TestBackgroundNotificationHelper$")
			cmd.Env = append(os.Environ(), "GEKTOR_BACKGROUND_HELPER=1")
			if outcome == "failed" {
				cmd.Env = append(cmd.Env, "GEKTOR_BACKGROUND_FAILURE=1")
			}
			id, _ := procs.start(ctx, cmd)
			procs.mu.Lock()
			done := procs.procs[id].done
			procs.mu.Unlock()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("helper never exited")
			}

			otherStore := tooloutput.New(t.TempDir())
			other := otherStore.Context(t.Context())
			notes := procs.Notifications(other)
			if len(notes) != 0 {
				t.Fatal("cross-session notification", notes)
			}
			notes = procs.Notifications(ctx)
			if len(notes) != 1 {
				t.Fatal(notes)
			}
			if len(notes[0]) > 200 || strings.Contains(notes[0], "measured background result") || !strings.Contains(notes[0], id+" "+outcome) || !strings.Contains(notes[0], "check_background") {
				t.Fatal("notification must contain only status and log retrieval guidance", notes)
			}
			if len(procs.Notifications(ctx)) != 0 {
				t.Fatal("completion delivered twice")
			}

			check := Check{Procs: procs}
			args := fmt.Sprintf(`{"id":%q,"cursor":0}`, id)
			output, err := check.Run(ctx, json.RawMessage(args))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, strings.Repeat("measured background result\n", 1000)) {
				t.Fatal("notification consumed retained logs")
			}
			if outcome == "failed" && !strings.Contains(output, "exit status 7") {
				t.Fatal("log retrieval lost failure detail", output)
			}
		})
	}
}
