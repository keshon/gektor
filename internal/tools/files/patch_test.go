package files

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

func TestPatchFile_ReplacesUniqueOccurrence(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	path := filepath.Join(dir, "game.js")
	if err := os.WriteFile(path, []byte("let score = 0;\nfunction draw() {}\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	tool := Patch{WS: ws}
	args, _ := json.Marshal(map[string]string{
		"path":        "game.js",
		"old_content": "let score = 0;",
		"new_content": "let score = 0;\nlet lives = 3;",
	})
	if _, err := tool.Run(context.Background(), args); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	want := "let score = 0;\nlet lives = 3;\nfunction draw() {}\n"
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestPatchFile_MissingContentErrors(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	os.WriteFile(filepath.Join(dir, "game.js"), []byte("let score = 0;\n"), 0o644)

	tool := Patch{WS: ws}
	args, _ := json.Marshal(map[string]string{
		"path":        "game.js",
		"old_content": "let lives = 3;",
		"new_content": "let lives = 5;",
	})
	if _, err := tool.Run(context.Background(), args); err == nil {
		t.Fatal("expected an error when old_content isn't found, got nil")
	}
}

func TestPatchFile_AmbiguousMatchErrors(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	os.WriteFile(filepath.Join(dir, "game.js"), []byte("x = 1;\nx = 1;\n"), 0o644)

	tool := Patch{WS: ws}
	args, _ := json.Marshal(map[string]string{
		"path":        "game.js",
		"old_content": "x = 1;",
		"new_content": "x = 2;",
	})
	if _, err := tool.Run(context.Background(), args); err == nil {
		t.Fatal("expected an error when old_content matches more than once, got nil")
	}
}

func TestPatchFile_UnknownFieldErrors(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	os.WriteFile(filepath.Join(dir, "game.js"), []byte("x\n"), 0o644)

	tool := Patch{WS: ws}
	args, _ := json.Marshal(map[string]string{"path": "game.js", "command": "sed"})
	if _, err := tool.Run(context.Background(), args); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestPatchPreservesCRLFReplacement(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	path := filepath.Join(dir, "x.txt")
	os.WriteFile(path, []byte("alpha\r\nbeta\r\n"), 0600)
	_, err := (Patch{WS: ws}).Run(context.Background(), json.RawMessage(`{"path":"x.txt","old_content":"beta","new_content":"new\r\nline"}`))
	body, _ := os.ReadFile(path)
	if err != nil || strings.Contains(string(body), "\r\r\n") || string(body) != "alpha\r\nnew\r\nline\r\n" {
		t.Fatalf("repro failed %q %v", body, err)
	}

}
