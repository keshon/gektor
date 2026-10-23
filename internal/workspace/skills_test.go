package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNestedSkillDiscoveryAndBoundedCatalog(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 80; i++ {
		dir := filepath.Join(root, "skills", "library", "engineering", fmt.Sprintf("skill-%02d", i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		guide := "---\ndescription: >-\n  " + strings.Repeat("description ", 20) + "\n---\nPRIVATE GUIDE BODY"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(guide), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	list, err := w.Skills()
	if err != nil || len(list) != 80 {
		t.Fatal(len(list), err)
	}
	if !strings.Contains(list[79].Path, "skill-79/") || !strings.Contains(list[79].Description, "description") {
		t.Fatal(list[79])
	}
	got := w.Instructions()
	if !strings.Contains(got, "80 workspace skills") || !strings.Contains(got, "group=skills") || strings.Contains(got, "PRIVATE GUIDE") || len(got) > 8192 {
		t.Fatal(got)
	}
	// Discovery is live; the cached prompt remains stable.
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(list[79].Path))); err != nil {
		t.Fatal(err)
	}
	list, err = w.Skills()
	if err != nil || len(list) != 79 || w.Instructions() != got {
		t.Fatal(len(list), err)
	}
}

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
