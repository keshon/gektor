package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
)

func imageWorkspace(t *testing.T) (*workspace.Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	ws, err := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	return ws, path
}

func TestReadFile_ImageAttachesOnlyWithVision(t *testing.T) {
	ws, path := imageWorkspace(t)
	args := json.RawMessage(`{"path":"shot.png"}`)

	out, err := (ReadFile{WS: ws, Vision: true}).Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"image: true", "format: png", "paging arguments do not apply"} {
		if !strings.Contains(out, want) {
			t.Fatalf("header missing %q:\n%s", want, out)
		}
	}
	// The tool cannot promise attachment: the harness drops pictures past
	// the per-step cap, and the tool result must not contradict that.
	if strings.Contains(out, "attached:") {
		t.Fatalf("vision header claims an attachment the tool cannot guarantee:\n%s", out)
	}
	got, err := (ReadFile{WS: ws, Vision: true}).Attach(args)
	if err != nil || len(got) != 1 || got[0] == path {
		t.Fatalf("Attach = %v, %v; want an immutable copy of %s", got, err, path)
	}
	original, _ := os.ReadFile(path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	copy, err := os.ReadFile(got[0])
	if err != nil || string(copy) != string(original) {
		t.Fatalf("snapshot was lost with source: %v", err)
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	out, err = (ReadFile{WS: ws}).Run(context.Background(), args)
	if err != nil {
		t.Fatalf("text-only Run: %v", err)
	}
	if !strings.Contains(out, "attached: false") || !strings.Contains(out, "cannot process pictures") {
		t.Fatalf("text-only header does not explain the missing pixels:\n%s", out)
	}
	if got, err := (ReadFile{WS: ws}).Attach(args); err != nil || len(got) != 0 {
		t.Fatalf("text-only Attach = %v, %v; want none", got, err)
	}
}

func TestReadFile_ImageMetadataOnlyDoesNotAttach(t *testing.T) {
	ws, _ := imageWorkspace(t)
	args := json.RawMessage(`{"path":"shot.png","metadata_only":true}`)
	r := ReadFile{WS: ws, Vision: true}
	out, err := r.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "attached: false (metadata_only)") {
		t.Fatalf("metadata header = %q", out)
	}
	if got, err := r.Attach(args); err != nil || len(got) != 0 {
		t.Fatalf("Attach = %v, %v; want none", got, err)
	}
}

func TestReadFile_AttachIgnoresTextFiles(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := (ReadFile{WS: ws, Vision: true}).Attach(json.RawMessage(`{"path":"notes.txt"}`))
	if err != nil || len(got) != 0 {
		t.Fatalf("Attach = %v, %v; want none", got, err)
	}
}

func TestReadFile_ImageOverCapFailsBeforeSend(t *testing.T) {
	ws, path := imageWorkspace(t)
	if err := os.Truncate(path, llm.MaxImageBytes+1); err != nil {
		t.Skipf("truncate: %v", err)
	}
	_, err := (ReadFile{WS: ws, Vision: true}).Run(context.Background(), json.RawMessage(`{"path":"shot.png"}`))
	if err == nil || !strings.Contains(err.Error(), "over the") {
		t.Fatalf("oversize image err = %v", err)
	}
}
