package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillCatalogLoadsMetadataNotInstructions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "custom")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: custom\ndescription: Inspect widgets.\n---\nPRIVATE FULL GUIDE\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := New(root)
	if w != nil {
		t.Cleanup(func() { w.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	got := w.Instructions()
	if !strings.Contains(got, "skills/custom/SKILL.md: Inspect widgets.") || strings.Contains(got, "PRIVATE FULL GUIDE") {
		t.Fatal(got)
	}
	// The catalog stays stable within a workspace lifetime (prompt cache).
	if err := os.Remove(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if w.Instructions() != got {
		t.Fatal("catalog changed")
	}
}
