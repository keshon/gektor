package background

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workspace"
)

func TestBackgroundStartupFailureIsNotSuccessfulToolResult(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	procs := New()
	defer procs.StopAll()
	out, err := (StartShell{WS: ws, Procs: procs, SettleTime: 3 * time.Second}).Run(t.Context(), json.RawMessage(`{"command":"exit 7"}`))
	if err == nil || !strings.Contains(out, "already exited") || !strings.Contains(err.Error(), "failed") {
		t.Fatal(out, err)
	}
}

func TestBackgroundProcesses_StartCheckStop(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	procs := New()
	defer procs.StopAll()

	start := StartShell{WS: ws, Procs: procs, SettleTime: 100 * time.Millisecond}
	args, _ := json.Marshal(map[string]string{"command": longRunningCommand})
	out, err := start.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(out, "running in background") {
		t.Fatalf("expected still running right after start, got: %q", out)
	}

	var id string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "id: ") {
			id = strings.TrimPrefix(line, "id: ")
		}
	}
	if id == "" {
		t.Fatalf("could not parse id from: %q", out)
	}
	if info, _, err := procs.Inspect(id); err != nil || info.Command != longRunningCommand {
		t.Fatal("lost original shell command", info, err)
	}

	check := Check{Procs: procs}
	cArgs, _ := json.Marshal(map[string]any{"id": id, "cursor": 0})
	checkOut, err := check.Run(context.Background(), cArgs)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(checkOut, "still running") {
		t.Fatalf("expected still running, got: %q", checkOut)
	}

	stop := Stop{Procs: procs}
	if _, err := stop.Run(context.Background(), json.RawMessage(`{"id":"`+id+`"}`)); err != nil {
		t.Fatalf("stop: %v", err)
	}

	time.Sleep(200 * time.Millisecond) // let Wait() observe the kill
	checkOut2, err := check.Run(context.Background(), cArgs)
	if err != nil {
		t.Fatalf("check after stop: %v", err)
	}
	if strings.Contains(checkOut2, "still running") {
		t.Fatalf("expected process to be stopped, got: %q", checkOut2)
	}
}

func TestBackgroundProcesses_CapturesOutput(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	procs := New()
	defer procs.StopAll()

	start := StartShell{WS: ws, Procs: procs, SettleTime: 300 * time.Millisecond}
	args, _ := json.Marshal(map[string]string{"command": "echo hello-from-bg"})
	out, err := start.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for !strings.Contains(out, "hello-from-bg") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		out, err = (Check{Procs: procs}).Run(context.Background(), json.RawMessage(`{"id":"bg1","cursor":0}`))
		if err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(out, "hello-from-bg") {
		t.Fatalf("expected captured output, got: %q", out)
	}
	var cursor int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "next_cursor:") {
			fmt.Sscanf(line, "next_cursor: %d", &cursor)
		}
	}
	args, _ = json.Marshal(map[string]any{"id": "bg1", "cursor": cursor})
	out, err = (Check{Procs: procs}).Run(t.Context(), args)
	if err != nil || cursor == 0 || strings.Contains(out, "hello-from-bg") {
		t.Fatalf("cursor repeated the startup output: %v %s", err, out)
	}
}

func TestCheckBackground_UnknownID(t *testing.T) {
	check := Check{Procs: New()}
	args, _ := json.Marshal(map[string]any{"id": "bg999", "cursor": 0})
	if _, err := check.Run(context.Background(), args); err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestBackgroundProcesses_StopKillsWholeProcessTree(t *testing.T) {
	// The tracked native wrapper starts a separate HTTP child. Verify the child
	// really serves before stopping its parent, and refuses connections after.
	// This exercises descendant cleanup on Windows and Unix without shell syntax.
	testManagedNativeHTTPServerLifecycle(t, []string{"--spawn-http-child"})
}
