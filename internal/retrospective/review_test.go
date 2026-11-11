package retrospective

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestEvaluationProgressFollowsObservedWork(t *testing.T) {
	c, _ := archiveFixture(t)
	c.Scope = "1"
	c.Client = reviewClient(t, 2, 4, false, nil)
	var updates []Progress
	c.OnProgress = func(p Progress) { updates = append(updates, p) }
	if _, err := Evaluate(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	if len(updates) == 0 || updates[0].Stage != "Loading saved archive" {
		t.Fatalf("missing archive load: %+v", updates)
	}
	waiting := map[int]bool{}
	read := false
	for _, p := range updates {
		if p.Stage == "Waiting for model response" {
			waiting[p.Step] = true
		}
		if p.Read == 4 && p.Total == 4 {
			read = true
		}
		if p.Read > p.Total || p.Step > p.Steps {
			t.Fatalf("invalid counters: %+v", p)
		}
	}
	if !waiting[1] || !waiting[2] || !waiting[3] || waiting[4] || !read {
		t.Fatalf("progress did not follow three requests and four archive excerpts: %+v", updates)
	}
	if last := updates[len(updates)-1]; last.Stage != "Saving evaluation" || last.Read != 4 {
		t.Fatalf("missing persistence phase: %+v", last)
	}
}

func TestEvaluateCompactedArchiveLeavesProposalOutsideSkills(t *testing.T) {
	c, _ := archiveFixture(t)
	original, _ := os.ReadFile(c.StateFile)
	c.Scope = "1"
	c.Client = reviewClient(t, 2, 4, true, nil)
	r, err := Evaluate(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Start != 2 || r.End != 5 || r.Read != 4 || r.Partial != 0 || r.SourceSHA256 == "" || r.Saved != "" || len(r.Memory) != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
	current, _ := os.ReadFile(c.StateFile)
	if string(current) != string(original) {
		t.Fatal("review changed original session")
	}
	if _, err := os.Stat(filepath.Join(c.Workspace.Root(), "skills")); !os.IsNotExist(err) {
		t.Fatalf("unapproved proposal changed workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.StateFile), "evaluation.json")); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateLastWithoutMemoryDoesNotCreateSkills(t *testing.T) {
	c, _ := archiveFixture(t)
	c.Client = reviewClient(t, 6, 8, false, nil)
	r, err := Evaluate(t.Context(), c)
	if err != nil || r.Start != 6 || r.End != 9 || r.Read != 4 || r.Saved != "" {
		t.Fatalf("last run: %+v %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(c.Workspace.Root(), "skills")); !os.IsNotExist(err) {
		t.Fatalf("empty memory changed workspace: %v", err)
	}
}

func TestEvaluateRejectsChangedArchiveAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed archive", true: "cancelled"}[cancel], func(t *testing.T) {
			c, _ := archiveFixture(t)
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			c.Scope = "1"
			c.Client = reviewClient(t, 2, 4, true, func() {
				if cancel {
					stop()
				} else if err := os.WriteFile(c.StateFile, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			})
			if _, err := Evaluate(ctx, c); err == nil {
				t.Fatal("accepted invalidated review")
			}
			for _, path := range []string{filepath.Join(filepath.Dir(c.StateFile), "evaluation.json"), filepath.Join(c.Workspace.Root(), "skills")} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("invalidated review wrote %s: %v", path, err)
				}
			}
		})
	}
}

func TestEvaluateRequiresSubmission(t *testing.T) {
	c, _ := archiveFixture(t)
	c.Client = clientFunc(func(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
		return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: "unsupported claim"}}, nil
	})
	if _, err := Evaluate(t.Context(), c); err == nil || !strings.Contains(err.Error(), "without submitting") {
		t.Fatalf("accepted unstructured assessment: %v", err)
	}
}
