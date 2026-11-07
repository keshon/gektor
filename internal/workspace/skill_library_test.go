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

func TestMalformedSkillsDoNotHideValidLibraries(t *testing.T) {
	root, library := t.TempDir(), t.TempDir()
	putSkill(t, library, "a-bad/SKILL.md", "---\nname: broken\ndescription: invalid: YAML\n---\nBAD")
	putSkill(t, library, "b-valid/SKILL.md", "---\nname: usable\ndescription: Library guide\n---\nVALID")
	putSkill(t, library, "b-valid/ref.txt", "RESOURCE")
	putSkill(t, root, "skills/project/SKILL.md", "---\nname: project\ndescription: Project guide\n---\nPROJECT")
	w, err := NewWithSkillRoots(root, []string{library})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	entries, warning := w.Skills()
	if len(entries) != 3 || warning == nil || !strings.Contains(warning.Error(), "a-bad") {
		t.Fatalf("entries=%v warning=%v", entries, warning)
	}
	if _, text, _, err := w.LoadSkill("usable"); err != nil || !strings.Contains(text, "VALID") {
		t.Fatalf("valid guide unavailable: %s %v", text, err)
	}
	if _, text, err := w.LoadSkillResource("usable", "ref.txt"); err != nil || text != "RESOURCE" {
		t.Fatalf("valid resource unavailable: %s %v", text, err)
	}
	if _, _, _, err := w.LoadSkill("broken"); err == nil || !strings.Contains(err.Error(), "frontmatter") {
		t.Fatal("missing invalid-guide diagnostic", err)
	}
	for _, marker := range []string{"usable:", "project:", "invalid entries skipped"} {
		if !strings.Contains(w.Instructions(), marker) {
			t.Fatalf("catalog missing %q", marker)
		}
	}
}

func TestSkillDescriptionLimitFromEnvironment(t *testing.T) {
	root := t.TempDir()
	putSkill(t, root, "skills/long/SKILL.md", "---\nname: long\ndescription: "+strings.Repeat("я", 1100)+"\n---\nGUIDE")
	for _, tc := range []struct {
		value string
		count int
	}{{"", 0}, {"1200", 1}, {"1099", 0}} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("GEKTOR_SKILL_DESCRIPTION_LIMIT", tc.value)
			w, err := New(root)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			entries, warning := w.Skills()
			if len(entries) != tc.count+1 || (warning != nil) != (tc.count == 0) {
				t.Fatalf("count=%d warning=%v", len(entries), warning)
			}
		})
	}
	for _, value := range []string{"0", "-1", "abc"} {
		t.Setenv("GEKTOR_SKILL_DESCRIPTION_LIMIT", value)
		if w, err := New(root); err == nil {
			w.Close()
			t.Fatalf("accepted invalid limit %q", value)
		}
	}
}

func TestDuplicateSkillDoesNotDisableUnrelatedSkill(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"one", "two"} {
		putSkill(t, root, "skills/"+dir+"/SKILL.md", "---\nname: duplicate\n---\nguide")
	}
	putSkill(t, root, "skills/valid/SKILL.md", "---\nname: valid\n---\nguide")
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	entries, warning := w.Skills()
	if warning == nil || len(entries) != 2 || entries[0].ID != "valid" {
		t.Fatalf("entries=%v warning=%v", entries, warning)
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
	if err != nil || len(entries) != 2 || entries[0].Description != "Ship: safely # exact" {
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
