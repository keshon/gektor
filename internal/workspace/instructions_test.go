package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNestedInstructionsStayInTheirScope(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "a", "inner"), 0700)
	_ = os.MkdirAll(filepath.Join(root, "b"), 0700)
	_ = os.WriteFile(filepath.Join(root, "a", "AGENTS.md"), []byte("A_RULE"), 0600)
	_ = os.WriteFile(filepath.Join(root, "a", "inner", "AGENTS.md"), []byte("INNER_RULE"), 0600)
	_ = os.WriteFile(filepath.Join(root, "b", "AGENTS.md"), []byte("B_RULE"), 0600)
	w, err := New(root)
	if w != nil {
		t.Cleanup(func() { w.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.ScopedInstructions("a/inner/new.go")
	if err != nil || !strings.Contains(a, "A_RULE") || !strings.Contains(a, "INNER_RULE") || strings.Contains(a, "B_RULE") {
		t.Fatal(a, err)
	}
	b, err := w.ScopedInstructions("b/new.go")
	if err != nil || strings.Contains(b, "A_RULE") || !strings.Contains(b, "B_RULE") {
		t.Fatal(b, err)
	}
}

func TestInstructionsLoadAncestorsBeforeRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	os.MkdirAll(root, 0755)
	os.WriteFile(filepath.Join(parent, "AGENTS.md"), []byte("outer rule"), 0600)
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("inner rule"), 0600)
	ws, err := New(root)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	text := ws.Instructions()
	if !strings.Contains(text, "outer rule") || strings.Index(text, "inner rule") < strings.Index(text, "outer rule") {
		t.Fatal(text)
	}
}
