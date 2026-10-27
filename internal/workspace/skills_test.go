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
	if !strings.Contains(got, "80 workspace skills") || !strings.Contains(got, "group=skills") || strings.Contains(got, "PRIVATE GUIDE") || len(got) > 2048 {
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
	if !strings.Contains(got, "custom: Inspect widgets.") || strings.Contains(got, "PRIVATE FULL GUIDE") {
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

func TestCompactSkillCatalogRetainsFullSearchMetadata(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills", "reader")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	description := strings.Repeat("word ", 80) + "TAIL_KEYWORD"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: reader\ndescription: "+description+"\n---\nFULL GUIDE"), 0600); err != nil {
		t.Fatal(err)
	}
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if strings.Contains(w.Instructions(), "TAIL_KEYWORD") || !strings.Contains(w.Instructions(), "reader:") || len(w.Instructions()) > 2048 {
		t.Fatal(w.Instructions())
	}
	entries, err := w.Skills()
	if err != nil || len(entries) != 1 || entries[0].Description != description {
		t.Fatal(entries, err)
	}
	_, guide, _, err := w.LoadSkill("reader")
	if err != nil || !strings.Contains(guide, "FULL GUIDE") || !strings.Contains(guide, "TAIL_KEYWORD") {
		t.Fatal(guide, err)
	}
}
