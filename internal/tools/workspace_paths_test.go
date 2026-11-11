package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/workspace"
)

func TestWorkspacePathsLoadsBothMoveScopesOnce(t *testing.T) {
	root := t.TempDir()
	for _, scope := range []string{"source", "target"} {
		dir := filepath.Join(root, scope)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("RULE_"+scope), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	paths := WorkspacePaths{WS: ws}
	text, err := paths.Instructions(json.RawMessage(`{"path":"source/a.txt","from":"source/b.txt","to":"target/b.txt"}`))
	if err != nil || strings.Count(text, "RULE_source") != 1 || strings.Count(text, "RULE_target") != 1 {
		t.Fatal(text, err)
	}
	for _, raw := range []string{`{`, `{"path":"attachment:1"}`, `{}`} {
		text, err := paths.Instructions(json.RawMessage(raw))
		if err != nil || text != "" {
			t.Fatal(raw, text, err)
		}
	}
	if _, err := paths.Instructions(json.RawMessage(`{"from":"source/a.txt","to":"../outside/a.txt"}`)); err == nil {
		t.Fatal("outside destination accepted")
	}
}
