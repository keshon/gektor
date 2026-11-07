package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledSkillsAreAvailableWithoutWorkspaceFiles(t *testing.T) {
	root := t.TempDir()
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, id := range []string{"workflows", "visual-investigation"} {
		skill, guide, hash, err := w.LoadSkill(id)
		if err != nil || skill.Source != "builtin" || skill.Path != "builtin:"+id+"/SKILL.md" || len(hash) != 64 || guide == "" {
			t.Fatal("bundled guide unavailable in empty workspace", skill, err)
		}
		if strings.Contains(w.Instructions(), guide) {
			t.Fatal("full guide entered initial prompt", id)
		}
		if _, _, err := w.LoadSkillResource(id, "../SKILL.md"); err == nil {
			t.Fatal("bundled package accepted a filesystem path", id)
		}
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatal("skill loading wrote to the workspace", files, err)
	}
}

func TestRepositoryAssetsOutsideSkillRootsAreNotDiscovered(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"internal/workspace/builtin/accidental", "eval/fixtures-skills/skills/release"} {
		putSkill(t, root, dir+"/SKILL.md", "---\nname: accidental\n---\nNot a project skill.")
	}
	w, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	entries, err := w.Skills()
	if err != nil || len(entries) != 2 {
		t.Fatal("source assets leaked into skill catalog", entries, err)
	}
}

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
