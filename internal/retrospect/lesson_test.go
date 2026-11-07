package retrospect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func proposedLesson(t *testing.T) (Config, Result) {
	t.Helper()
	c, _ := archiveFixture(t)
	c.Scope = "1"
	c.Client = reviewClient(t, 2, 4, true, nil)
	r, err := Evaluate(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	return c, r
}

func TestLessonApprovalDiscoveryDeduplicationAndRetirement(t *testing.T) {
	c, r := proposedLesson(t)
	root := c.Workspace.Root()
	_, before := c.Workspace.InstructionSections()
	if strings.Contains(before, "learned-") {
		t.Fatal("unapproved lesson in catalog")
	}
	when := "When changing the API: run tests"
	procedure := "Run go test ./... inside the API module."
	lesson, err := Approve(t.Context(), root, c.StateFile, r, when, procedure)
	if err != nil || !lesson.Created {
		t.Fatalf("approve: %+v %v", lesson, err)
	}
	entries, err := c.Workspace.Skills()
	if err != nil || len(entries) != 3 || entries[0].Description != when {
		t.Fatalf("discovery: %+v %v", entries, err)
	}
	_, metadata := c.Workspace.InstructionSections()
	if !strings.Contains(metadata, when) {
		t.Fatal("next task sees stale catalog")
	}
	_, guide, _, err := c.Workspace.LoadSkill(entries[0].ID)
	if err != nil || !strings.Contains(guide, procedure) {
		t.Fatalf("load approved edit: %s %v", guide, err)
	}
	pinned, err := os.ReadFile(filepath.Join(root, filepath.Dir(lesson.Path), "evidence.json"))
	if err != nil || !strings.Contains(string(pinned), r.SourceSHA256) || !strings.Contains(string(pinned), "PASS first verification") || strings.Contains(string(pinned), "original system") {
		t.Fatalf("pinned evidence: %s %v", pinned, err)
	}
	duplicate, err := Approve(t.Context(), root, c.StateFile, r, when, procedure)
	if err != nil || duplicate.Created || duplicate.Path != lesson.Path {
		t.Fatalf("duplicate: %+v %v", duplicate, err)
	}
	if err := Undo(root, duplicate); err == nil {
		t.Fatal("duplicate save could retire existing lesson")
	}
	if err := Undo(root, lesson); err != nil {
		t.Fatal(err)
	}
	_, metadata = c.Workspace.InstructionSections()
	if strings.Contains(metadata, entries[0].ID) {
		t.Fatal("retired lesson remains in catalog")
	}
	if _, _, _, err := c.Workspace.LoadSkill(entries[0].ID); err == nil {
		t.Fatal("retired lesson remains loadable")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.Dir(lesson.Path), "evidence.json")); err != nil {
		t.Fatal("undo lost evidence", err)
	}
	restored, err := Approve(t.Context(), root, c.StateFile, r, when, procedure)
	if err != nil || !restored.Created {
		t.Fatalf("explicit reapproval: %+v %v", restored, err)
	}
}

func TestLessonRejectsChangedArchiveAndInvalidEvidence(t *testing.T) {
	for _, test := range []string{"archive", "quote", "index", "empty", "oversize"} {
		t.Run(test, func(t *testing.T) {
			c, r := proposedLesson(t)
			when, procedure := "API changes", "Run module tests."
			switch test {
			case "archive":
				if err := os.WriteFile(c.StateFile, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "quote":
				r.Memory[0].Quote = "invented success"
			case "index":
				r.Memory[0].Evidence = []int{999}
			case "empty":
				when = " "
			case "oversize":
				procedure = strings.Repeat("x", 1001)
			}
			if _, err := Approve(t.Context(), c.Workspace.Root(), c.StateFile, r, when, procedure); err == nil {
				t.Fatal("accepted invalid lesson")
			}
			entries, err := c.Workspace.Skills()
			if err != nil || len(entries) != 2 {
				t.Fatalf("failed approval published: %+v %v", entries, err)
			}
		})
	}
}

func TestLessonPreservesUserEdits(t *testing.T) {
	c, r := proposedLesson(t)
	root := c.Workspace.Root()
	memory := r.Memory[0]
	lesson, err := Approve(t.Context(), root, c.StateFile, r, memory.When, memory.Lesson)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, lesson.Path)
	if err := os.WriteFile(path, []byte("My corrected lesson"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Undo(root, lesson); err == nil {
		t.Fatal("undo removed edited guide")
	}
	if _, err := Approve(t.Context(), root, c.StateFile, r, memory.When, memory.Lesson); err == nil {
		t.Fatal("save overwrote edited guide")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "My corrected lesson" {
		t.Fatal("lost user edits")
	}
}
