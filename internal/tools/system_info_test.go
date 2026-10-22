package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

func TestSystemInfoDiscoveryAndPaging(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, dir := range []string{a, b} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".EXE"
		t.Setenv("PATHEXT", ".EXE;.CMD")
	}
	for _, path := range []string{filepath.Join(a, "unlisted-program"+ext), filepath.Join(b, "unlisted-program"+ext), filepath.Join(a, "other"+ext)} {
		if err := os.WriteFile(path, []byte("must not execute"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(a, "notes.txt"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", strings.Join([]string{a, b, a, filepath.Join(root, "missing")}, string(os.PathListSeparator)))
	t.Setenv("PRIVATE_TOKEN", "must-not-leak")
	ws, err := workspace.New(root)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	tool := SystemInfo{WS: ws}
	out, err := tool.Run(t.Context(), json.RawMessage(`{"group":"tools","filter":"UNLISTED","limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Tools      []pathTool
		Total      int
		NextOffset int `json:"next_offset"`
		Warnings   []string
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Tools) != 1 || page.NextOffset != 1 || len(page.Warnings) != 1 {
		t.Fatal(out)
	}
	first := page.Tools[0].Path
	out, err = tool.Run(t.Context(), json.RawMessage(`{"group":"tools","filter":"unlisted","offset":1,"limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatal(err)
	}
	if page.Tools[0].Path == first || strings.Contains(out, "next_offset") {
		t.Fatal(out)
	}
	out, err = tool.Run(t.Context(), json.RawMessage(`{"group":"environment"}`))
	if err != nil || strings.Contains(out, "must-not-leak") {
		t.Fatal(out, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := tool.Run(ctx, json.RawMessage(`{"group":"tools"}`)); err == nil {
		t.Fatal("cancelled discovery succeeded")
	}
}

func TestSystemInfoRejectsInvalidRequests(t *testing.T) {
	for _, args := range []string{`{}`, `{"group":"unknown"}`, `{"group":"tools","offset":-1}`, `{"group":"tools","limit":0}`, `{"group":"tools","limit":101}`, `{"group":"os","filter":"go"}`} {
		if _, err := (SystemInfo{}).Run(t.Context(), json.RawMessage(args)); err == nil {
			t.Fatal(args)
		}
	}
}
