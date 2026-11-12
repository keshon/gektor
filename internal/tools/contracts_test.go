package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toolfiles "github.com/keshon/gektor/internal/tools/files"
	"github.com/keshon/gektor/internal/workspace"
)

func TestPatchPreservesCRLFReplacement(t *testing.T) {
	dir := t.TempDir()
	ws, _ := workspace.New(dir)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	path := filepath.Join(dir, "x.txt")
	os.WriteFile(path, []byte("alpha\r\nbeta\r\n"), 0600)
	_, err := (toolfiles.Patch{WS: ws}).Run(context.Background(), json.RawMessage(`{"path":"x.txt","old_content":"beta","new_content":"new\r\nline"}`))
	body, _ := os.ReadFile(path)
	if err != nil || strings.Contains(string(body), "\r\r\n") || string(body) != "alpha\r\nnew\r\nline\r\n" {
		t.Fatalf("repro failed %q %v", body, err)
	}

}
