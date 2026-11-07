package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledWorkflowSkillAndProjectOverride(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	skill, guide, hash, err := w.LoadSkill("workflows")
	if err != nil || skill.Source != "builtin" || hash == "" || !strings.Contains(guide, "script_transform") {
		t.Fatal(skill, guide, err)
	}
	if _, _, err := w.LoadSkillResource("workflows", "../secret"); err == nil {
		t.Fatal("accepted unknown bundled resource")
	}
	if err := os.MkdirAll(filepath.Join(w.Root(), "skills", "workflows"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteFile("skills/workflows/SKILL.md", []byte("---\nname: workflows\ndescription: Project workflow guide.\n---\nProject override."), 0600); err != nil {
		t.Fatal(err)
	}
	skill, guide, _, err = w.LoadSkill("workflows")
	if err != nil || skill.Source == "builtin" || !strings.Contains(guide, "Project override") {
		t.Fatal(skill, guide, err)
	}
}
