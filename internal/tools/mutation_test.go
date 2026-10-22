package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

func testWS(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	ws, err := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	return ws, dir
}

func writeTmp(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runTool(t *testing.T, tool interface {
	Run(context.Context, json.RawMessage) (string, error)
}, args string) (string, error) {
	t.Helper()
	return tool.Run(context.Background(), json.RawMessage(args))
}

func TestPatchFile_EmptyOldContentRejected(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, "a.txt", "one\ntwo\nthree\nfour\nfive\n")
	_, err := runTool(t, PatchFile{WS: ws}, `{"path":"a.txt","old_content":"","new_content":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty rejection, got %v", err)
	}
}

func TestPatchFile_IdenticalRejected(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, "a.txt", "one\ntwo\nthree\nfour\nfive\n")
	_, err := runTool(t, PatchFile{WS: ws}, `{"path":"a.txt","old_content":"two","new_content":"two"}`)
	if err == nil || !strings.Contains(err.Error(), "identical") {
		t.Fatalf("expected identical rejection, got %v", err)
	}
}

func TestPatchFile_OversizedNewContentRejected(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, "a.txt", "one\ntwo\nthree\nfour\nfive\n")
	big := strings.Repeat("x", writeMaxBytes+1)
	_, err := runTool(t, PatchFile{WS: ws}, `{"path":"a.txt","old_content":"two","new_content":"`+big+`"}`)
	if err == nil || !strings.Contains(err.Error(), "write cap") {
		t.Fatalf("expected write-cap refusal, got %v", err)
	}
}

func TestPatchFile_ReturnsDiff(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, "a.txt", "one\ntwo\nthree\nfour\nfive\nsix\n")
	out, err := runTool(t, PatchFile{WS: ws}, `{"path":"a.txt","old_content":"three","new_content":"THREE"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "-three") || !strings.Contains(out, "+THREE") || !strings.Contains(out, "@@") {
		t.Fatalf("expected unified diff, got %q", out)
	}
}

func TestPatchFile_CRLFPreserved(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, "a.txt", "one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\n")
	_, err := runTool(t, PatchFile{WS: ws}, `{"path":"a.txt","old_content":"three","new_content":"THREE"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if !strings.Contains(string(data), "\r\n") || !strings.Contains(string(data), "THREE") {
		t.Fatalf("CRLF not preserved: %q", data)
	}
}

func TestWriteFile_RejectsUnknownFields(t *testing.T) {
	ws, _ := testWS(t)
	_, err := runTool(t, WriteFile{WS: ws}, `{"path":"a.txt","content":"x","command":"y"}`)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field rejection, got %v", err)
	}
}

func TestMoveFile_RefusesOverwrite(t *testing.T) {
	ws, dir := testWS(t)
	writeTmp(t, dir, "a.txt", "a")
	writeTmp(t, dir, "b.txt", "b")
	_, err := runTool(t, MoveFile{WS: ws}, `{"from":"a.txt","to":"b.txt"}`)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
}

func TestPathLocks_Serialize(t *testing.T) {
	var mu sync.Mutex
	order := []int{}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = withPathLock("same.txt", func() (string, error) {
				mu.Lock()
				order = append(order, i)
				mu.Unlock()
				return "ok", nil
			})
		}(i)
	}
	wg.Wait()
	if len(order) != 10 {
		t.Fatalf("order=%v", order)
	}
}

func TestPatchDiffAppliesWithGitIncludingEOFChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, pair := range [][2]string{
		{"one\ntwo\nthree\n", "one\nTWO\nthree\n"},
		{"one\ntwo\n", "one\n"},
		{"one\n", ""}, {"", "one\n"},
		{"one", "one\n"}, {"one\n", "one"},
		{"one", "one\ntwo"},
		{strings.Repeat("unchanged\n", 149) + "old\n", strings.Repeat("unchanged\n", 149) + "new\n"},
	} {
		t.Run(fmt.Sprintf("%d-%d-%q", len(pair[0]), len(pair[1]), pair[0][:min(4, len(pair[0]))]), func(t *testing.T) {
			dir := t.TempDir()
			writeTmp(t, dir, "file.txt", pair[0])
			patch := unifiedDiff("file.txt", pair[0], pair[1])
			cmd := exec.Command("git", "-c", "core.autocrlf=false", "apply", "-")
			cmd.Dir = dir
			cmd.Stdin = strings.NewReader(patch)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("diff is not applicable: %v %s\n%s", err, out, patch)
			}
			got, _ := os.ReadFile(filepath.Join(dir, "file.txt"))
			if string(got) != pair[1] {
				t.Fatalf("diff produced %q, expected %q", got, pair[1])
			}
		})
	}
}

func TestPatchFileStableAnchorsAndRefusalPreserveBytes(t *testing.T) {
	ws, dir := testWS(t)
	before := "\xef\xbb\xbfpackage example\r\n\r\nfunc first() {}\r\nfunc second() {}\r\n"
	writeTmp(t, dir, "file.go", before)
	tool := PatchFile{WS: ws}
	patch := func(old, replacement string) error {
		args, _ := json.Marshal(map[string]string{"path": "file.go", "old_content": old, "new_content": replacement})
		_, err := tool.Run(t.Context(), args)
		return err
	}
	if err := patch("func first() {}", "// inserted before the second function\nfunc first() {}"); err != nil {
		t.Fatal(err)
	}
	if err := patch("func second() {}", "func second() {\n}\n"); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(filepath.Join(dir, "file.go"))
	if !strings.HasPrefix(string(current), "\xef\xbb\xbf") || !strings.Contains(string(current), "\r\n") {
		t.Fatalf("lost BOM or CRLF: %q", current)
	}
	for _, old := range []string{"func second() {}", "{}"} {
		// Missing or ambiguous matches must not touch the file.
		if old == "{}" {
			writeTmp(t, dir, "file.go", "{}\r\n{}\r\n")
			current, _ = os.ReadFile(filepath.Join(dir, "file.go"))
		}
		if err := patch(old, "replacement"); err == nil {
			t.Fatalf("unsafe match accepted: %q", old)
		}
		got, _ := os.ReadFile(filepath.Join(dir, "file.go"))
		if string(got) != string(current) {
			t.Fatal("refused patch changed the file")
		}
	}
}
