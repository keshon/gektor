package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putSkill(t *testing.T, root, relative, content string) {
	t.Helper()
	file := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSkillLibrariesPrecedenceAndResources(t *testing.T) {
	root, library := t.TempDir(), t.TempDir()
	putSkill(t, library, "engineering/release/SKILL.md", "---\nname: release\ndescription: 'Ship: safely # exact'\n---\nLIBRARY_ONLY_BODY")
	putSkill(t, library, "engineering/release/references/checks.txt", "library checks")
	w, err := NewWithSkillRoots(root, []string{filepath.Join(library, "missing"), library})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	entries, err := w.Skills()
	if err != nil || len(entries) != 1 || entries[0].Description != "Ship: safely # exact" {
		t.Fatal(entries, err)
	}
	if strings.Contains(w.Instructions(), "LIBRARY_ONLY_BODY") {
		t.Fatal("guide eagerly loaded")
	}
	skill, text, digest, err := w.LoadSkill("release")
	if err != nil || !strings.Contains(text, "LIBRARY_ONLY_BODY") || len(digest) != 64 || !filepath.IsAbs(skill.BaseDir) {
		t.Fatal(skill, text, digest, err)
	}
	if _, text, err := w.LoadSkillResource("release", "references/checks.txt"); err != nil || text != "library checks" {
		t.Fatal(text, err)
	}
	for _, escape := range []string{"../SKILL.md", filepath.Join(library, "engineering/release/SKILL.md")} {
		if _, _, err := w.LoadSkillResource("release", escape); err == nil {
			t.Fatal("resource escaped package", escape)
		}
	}
	putSkill(t, root, "skills/release/SKILL.md", "---\nname: release\ndescription: Project workflow\n---\nPROJECT_BODY")
	if skill, text, _, err := w.LoadSkill("release"); err != nil || skill.Path != "skills/release/SKILL.md" || !strings.Contains(text, "PROJECT_BODY") {
		t.Fatal(skill, text, err)
	}
	putSkill(t, root, ".agents/skills/release/SKILL.md", "---\nname: release\ndescription: Agent workflow\n---\nAGENT_BODY")
	if _, text, _, err := w.LoadSkill("release"); err != nil || !strings.Contains(text, "AGENT_BODY") {
		t.Fatal(text, err)
	}
}

func TestSkillFrontmatterValidationAndDescriptionLength(t *testing.T) {
	description := strings.Repeat("routing detail ", 50)
	meta, err := readSkillMetadata(strings.NewReader("---\nname: valid-skill\ndescription: |\n  " + description + "\n---\nBODY"))
	if err != nil || len(meta.Description) < 220 {
		t.Fatal("description truncated", meta, err)
	}
	for _, text := range []string{
		"---\nname: Invalid_Name\n---",
		"---\ndescription: [not, a, string]\n---",
		"---\ndescription: first\ndescription: duplicate\n---",
		"---\ndescription: " + strings.Repeat("x", 17000),
		"---\nname: valid\n",
	} {
		if _, err := readSkillMetadata(strings.NewReader(text)); err == nil {
			t.Fatal("invalid metadata accepted", text[:min(80, len(text))])
		}
	}
}

func TestSkillResourcesDoNotFollowEscapingSymlinks(t *testing.T) {
	root, library, outside := t.TempDir(), t.TempDir(), t.TempDir()
	putSkill(t, library, "release/SKILL.md", "---\nname: release\ndescription: Ship\n---\nguide")
	putSkill(t, outside, "private.txt", "outside package")
	if err := os.Symlink(filepath.Join(outside, "private.txt"), filepath.Join(library, "release", "escape.txt")); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	w, err := NewWithSkillRoots(root, []string{library})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, _, err := w.LoadSkillResource("release", "escape.txt"); err == nil {
		t.Fatal("symlink escaped skill package")
	}
}
