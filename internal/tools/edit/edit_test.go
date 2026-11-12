package edit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

func TestWriteFile_AllowsGenuineRewritesAndNewFiles(t *testing.T) {
	dir := t.TempDir()
	ws, err := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	w := Write{WS: ws}

	// A new file is never a rewrite.
	args, _ := json.Marshal(map[string]string{
		"path": "new.go", "content": "package a\n\nfunc A() {}\n"})
	if _, err := w.Run(context.Background(), args); err != nil {
		t.Fatalf("new file refused: %v", err)
	}

	// Replacing a file with genuinely different content is the case
	// write_file exists for.
	long := "package a\n"
	for i := 0; i < 20; i++ {
		long += fmt.Sprintf("// original line %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(long), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	replacement := "package b\n"
	for i := 0; i < 20; i++ {
		replacement += fmt.Sprintf("// completely different %d\n", i)
	}
	args, _ = json.Marshal(map[string]string{"path": "big.go", "content": replacement})
	if _, err := w.Run(context.Background(), args); err != nil {
		t.Fatalf("genuine rewrite refused: %v", err)
	}
}

func TestWriteFile_OversizedContentRefused(t *testing.T) {
	dir := t.TempDir()
	ws, err := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	big := strings.Repeat("x", writeMaxBytes+1)
	args, _ := json.Marshal(map[string]string{"path": "big.go", "content": big})
	if _, err := (Write{WS: ws}).Run(context.Background(), args); err == nil ||
		!strings.Contains(err.Error(), "write cap") {
		t.Fatalf("expected write-cap refusal, got %v", err)
	}
	if _, stat := os.Stat(filepath.Join(dir, "big.go")); !os.IsNotExist(stat) {
		t.Fatal("refused write must not touch disk")
	}
}

func TestAmbiguousPatchReportsLocationsAndLeavesFileUnchanged(t *testing.T) {
	root := t.TempDir()
	ws, err := workspace.New(root)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	content := "before\r\ntarget\r\nmiddle\r\ntarget\r\nafter\r\n"
	path := filepath.Join(root, "data.txt")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = (Patch{WS: ws}).Run(t.Context(), json.RawMessage(`{"path":"data.txt","old_content":"target","new_content":"changed"}`))
	if err == nil || !strings.Contains(err.Error(), "starting lines 2, 4") {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Fatal("ambiguous patch changed file", err)
	}
	_, err = (Patch{WS: ws}).Run(t.Context(), json.RawMessage(`{"path":"data.txt","old_content":"middle\ntarget","new_content":"middle\nchanged"}`))
	if err != nil {
		t.Fatal("could not recover with context", err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != "before\r\ntarget\r\nmiddle\r\nchanged\r\nafter\r\n" {
		t.Fatal(string(got), err)
	}
}
