package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

// Live (probe 10): read_file was capped at 48KB an hour earlier, so the
// model reached for "type big.txt" instead and put a 205KB fixture into
// the prompt whole — 3.4k to 57k tokens in one step. Capping the
// sanctioned route while leaving the shell uncapped only moved the
// traffic.
func TestRunShell_CapsHugeOutput(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("abcdefgh", 40*1024) // 320KB
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}

	cmd := "cat big.txt"
	if runtime.GOOS == "windows" {
		cmd = "type big.txt"
	}
	args, _ := json.Marshal(map[string]string{"command": cmd})
	out, err := (RunShell{WS: ws}).Run(context.Background(), args)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(out) > shellMaxBytes+512 {
		t.Errorf("run_shell returned %d bytes, cap is %d", len(out), shellMaxBytes)
	}
	if !strings.Contains(out, "output truncated") {
		t.Error("a truncated result must say so, or the model trusts it as complete")
	}
}

func TestRunShell_LeavesNormalOutputAlone(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	args, _ := json.Marshal(map[string]string{"command": "echo hello"})
	out, err := (RunShell{WS: ws}).Run(context.Background(), args)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hello") || strings.Contains(out, "truncated") {
		t.Errorf("ordinary output was altered: %q", out)
	}
}

func TestRunShellUsesWorkspaceRelativeWorkdir(t *testing.T) {
	ws, root := testWS(t)
	child := filepath.Join(root, "package with spaces")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	writeTmp(t, root, "marker.txt", "root marker")
	writeTmp(t, child, "marker.txt", "child marker")
	command := "cat marker.txt"
	if runtime.GOOS == "windows" {
		command = "type marker.txt"
	}
	for _, dir := range []string{"", "package with spaces"} {
		args, _ := json.Marshal(map[string]any{"command": command, "workdir": dir})
		out, err := (RunShell{WS: ws}).Run(t.Context(), args)
		want := "root marker"
		if dir != "" {
			want = "child marker"
		}
		if err != nil || strings.TrimSpace(out) != want {
			t.Fatalf("workdir=%q: %q %v", dir, out, err)
		}
	}
}

func TestRunShellValidatesBoundsBeforeStarting(t *testing.T) {
	ws, root := testWS(t)
	for _, fields := range []map[string]any{
		{"workdir": ".."}, {"timeout_seconds": 0}, {"timeout_seconds": -1}, {"timeout_seconds": 601}, {"timeout": 1},
	} {
		fields["command"] = "echo unexpected > should-not-run.txt"
		args, _ := json.Marshal(fields)
		if _, err := (RunShell{WS: ws}).Run(t.Context(), args); err == nil {
			t.Fatalf("invalid arguments executed: %s", args)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "should-not-run.txt")); !os.IsNotExist(err) {
		t.Fatal("validation happened after command execution")
	}
}

func TestRunShellHonorsCommandDeadline(t *testing.T) {
	ws, _ := testWS(t)
	args, _ := json.Marshal(map[string]any{"command": longRunningCommand, "timeout_seconds": 1})
	if _, err := (RunShell{WS: ws}).Run(t.Context(), args); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("command deadline was not enforced: %v", err)
	}
}
