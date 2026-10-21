package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitCommitMessageCreditAndTemporaryFileLifetime(t *testing.T) {
	dir := initGitRepo(t, t.TempDir())
	tool := newGitTool(t, dir)
	temp := filepath.Join(t.TempDir(), "message files with spaces")
	if err := os.Mkdir(temp, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TMP", "TEMP", "TMPDIR"} {
		t.Setenv(name, temp)
	}
	t.Setenv("GIT_AUTHOR_NAME", "Human author")
	t.Setenv("GIT_AUTHOR_EMAIL", "human@example.test")
	for i, message := range []string{
		"Fix parsing\n\nKeep 100% of & pipes | quotes \" and русский текст.",
		"Next change\n\n" + gitAttribution,
	} {
		writeTmp(t, dir, "hello.txt", message)
		if _, err := (GitAdd{WS: tool.WS}).Run(t.Context(), json.RawMessage(`{"path":"hello.txt"}`)); err != nil {
			t.Fatal(err)
		}
		args, _ := json.Marshal(map[string]string{"message": message})
		if out, err := (GitCommit{WS: tool.WS}).Run(t.Context(), args); err != nil {
			t.Fatalf("commit %d failed: %v %s", i, err, out)
		}
		cmd := exec.Command("git", "show", "-s", "--format=%an <%ae>%n%B", "HEAD")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil || !strings.Contains(string(out), "Human author <human@example.test>") || !strings.Contains(string(out), message) || strings.Count(string(out), gitAttribution) != 1 {
			t.Fatalf("author/message changed or attribution duplicated: %v %s", err, out)
		}
		files, _ := filepath.Glob(filepath.Join(temp, "gektor-commit-*.txt"))
		if len(files) != 0 {
			t.Fatalf("message file survived successful commit: %v", files)
		}
	}
	// A failed commit must clean up synchronously too, without a timer.
	args, _ := json.Marshal(map[string]string{"message": "Nothing staged"})
	if _, err := (GitCommit{WS: tool.WS}).Run(t.Context(), args); err == nil {
		t.Fatal("empty index unexpectedly committed")
	}
	files, _ := filepath.Glob(filepath.Join(temp, "gektor-commit-*.txt"))
	if len(files) != 0 {
		t.Fatalf("message file survived failed commit: %v", files)
	}
}

func TestGitWriteRefusesInvalidArguments(t *testing.T) {
	ws, _ := testWS(t)
	for _, args := range []string{`{}`, `{"path":""}`, `{"path":"../escape"}`, `{"path":"file","force":true}`} {
		if _, err := (GitAdd{WS: ws}).Run(t.Context(), json.RawMessage(args)); err == nil {
			t.Fatalf("add accepted %s", args)
		}
	}
	for _, args := range []string{`{}`, `{"message":" "}`, `{"message":"x","amend":true}`} {
		if _, err := (GitCommit{WS: ws}).Run(t.Context(), json.RawMessage(args)); err == nil {
			t.Fatalf("commit accepted %s", args)
		}
	}
}
