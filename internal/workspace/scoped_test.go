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

func TestSkillCatalogUnderstandsMultilineDescription(t *testing.T) {
	for _, style := range []string{">-", "|-"} {
		root := t.TempDir()
		dir := filepath.Join(root, "skills", "test")
		_ = os.MkdirAll(dir, 0700)
		_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: test\ndescription: "+style+"\n  Read the actual browser DOM.\n  Check console errors.\n---\nFULL_BODY_MUST_STAY_UNLOADED"), 0600)
		w, err := New(root)
		if err != nil {
			t.Fatal(err)
		}
		text := w.Instructions()
		if !strings.Contains(text, "Read the actual browser DOM. Check console errors.") || strings.Contains(text, "FULL_BODY_MUST_STAY_UNLOADED") {
			t.Fatal(text)
		}
	}
}
