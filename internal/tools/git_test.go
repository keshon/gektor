package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

// initGitRepo creates a temporary git repository inside dir and returns its
// root. It is skipped when git is not installed so the suite stays green on
// machines without a VCS.
func initGitRepo(t *testing.T, dir string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed; skipping git tool test")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	run("add", "hello.txt")
	run("commit", "-m", "initial")
	return dir
}

func newGitTool(t *testing.T, dir string) Git {
	t.Helper()
	ws, err := workspace.New(dir)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	return Git{WS: ws}
}

func TestGit_Log(t *testing.T) {
	dir := initGitRepo(t, t.TempDir())
	tool := newGitTool(t, dir)

	args, _ := json.Marshal(map[string]string{"subcommand": "log"})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run log: %v", err)
	}
	if !strings.Contains(out, "initial") {
		t.Fatalf("log output missing commit message, got %q", out)
	}
}

func TestGit_Status(t *testing.T) {
	dir := initGitRepo(t, t.TempDir())
	tool := newGitTool(t, dir)

	// A clean tree reports nothing under porcelain status.
	args, _ := json.Marshal(map[string]string{"subcommand": "status"})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run status: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected clean status, got %q", out)
	}

	// After adding an untracked file, status should show it.
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("seed untracked: %v", err)
	}
	out, err = tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run status after add: %v", err)
	}
	if !strings.Contains(out, "new.txt") {
		t.Fatalf("expected untracked file in status, got %q", out)
	}
}

func TestGit_ShowRequiresRef(t *testing.T) {
	dir := initGitRepo(t, t.TempDir())
	tool := newGitTool(t, dir)

	args, _ := json.Marshal(map[string]string{"subcommand": "show"})
	if _, err := tool.Run(context.Background(), args); err == nil {
		t.Fatal("expected an error for show without a ref, got nil")
	}
}

func TestGit_UnknownSubcommandErrors(t *testing.T) {
	dir := initGitRepo(t, t.TempDir())
	tool := newGitTool(t, dir)

	args, _ := json.Marshal(map[string]string{"subcommand": "explode"})
	if _, err := tool.Run(context.Background(), args); err == nil {
		t.Fatal("expected an error for an unknown subcommand, got nil")
	}
}

func TestGit_RejectsEscapedPath(t *testing.T) {
	dir := initGitRepo(t, t.TempDir())
	tool := newGitTool(t, dir)

	// A path that escapes the workspace must be refused before git runs,
	// not silently scoped to something outside the sandbox.
	args, _ := json.Marshal(map[string]string{"subcommand": "ls-files", "path": "../outside"})
	if _, err := tool.Run(context.Background(), args); err == nil {
		t.Fatal("expected an error for a path escaping the workspace, got nil")
	}
}
