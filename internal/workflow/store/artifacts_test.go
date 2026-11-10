package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func TestArtifactsDurabilityIsolationAndIntegrity(t *testing.T) {
	s, base, work := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	a, err := w.Artifacts("import").Put(t.Context(), "picture.bin", bytes.NewReader([]byte{0, 255, 42}))
	if err != nil {
		t.Fatal(err)
	}
	if a.StepID != "import" || a.Size != 3 || a.RunID != w.ID() {
		t.Fatal(a)
	}
	id := w.ID()
	w.Close()
	reopened, err := OpenStore(base, work)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	w, _, err = reopened.EditRun(id)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	_, data, err := w.Artifacts("reader").Read(t.Context(), a.ID)
	if err != nil || !bytes.Equal(data, []byte{0, 255, 42}) {
		t.Fatal(data, err)
	}
	other, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, _, err := other.Artifacts("reader").Read(t.Context(), a.ID); err == nil {
		t.Fatal("cross-run artifact access")
	}
	if _, _, err := w.Artifacts("reader").Read(t.Context(), "../"+a.ID); err == nil {
		t.Fatal("path accepted as ID")
	}
	path := filepath.Join(s.path, "runs", id, "artifacts", a.ID, "data")
	if err := os.WriteFile(path, []byte{1, 2, 3}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.Artifacts("reader").Read(t.Context(), a.ID); err == nil {
		t.Fatal("tampered bytes accepted")
	}
}

func TestArtifactsBoundsAndCancelledPublication(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	a := w.Artifacts("save")
	if _, err := a.Put(t.Context(), "../bad", strings.NewReader("x")); err == nil {
		t.Fatal("invalid name accepted")
	}
	if _, err := a.Put(t.Context(), "huge", ioZeros{}); err == nil {
		t.Fatal("oversized artifact accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.Put(ctx, "cancelled", strings.NewReader("x")); err == nil {
		t.Fatal("cancelled publish accepted")
	}
	items, err := w.ListArtifacts()
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	f, err := s.root.Open(w.artifactDirectory())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil || len(entries) != 0 {
		t.Fatal("temporary files left", entries, err)
	}
	w.Close()
	if _, err := a.Put(t.Context(), "closed", strings.NewReader("x")); err == nil {
		t.Fatal("closed lease accepted")
	}
}

type ioZeros struct{}

func (ioZeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestArtifactRetentionPreservesReviewAndActiveRuns(t *testing.T) {
	s, _, _ := testStore(t)
	old := time.Now().Add(-48 * time.Hour)
	ids := map[string]string{}
	for _, status := range []string{"completed", "waiting_review", "running", "failed"} {
		w, err := s.NewRun()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Artifacts("save").Put(t.Context(), "file", strings.NewReader("data")); err != nil {
			t.Fatal(err)
		}
		if err := w.Save(workflow.Run{Status: status, Finished: &old}); err != nil {
			t.Fatal(err)
		}
		ids[status] = w.ID()
		if status == "failed" {
			defer w.Close()
		} else {
			w.Close()
		}
	}
	removed, err := s.PruneArtifacts(t.Context(), time.Now().Add(-24*time.Hour))
	if err != nil || len(removed) != 1 || removed[0] != ids["completed"] {
		t.Fatal(removed, err)
	}
	for status, id := range ids {
		_, err := s.root.Stat(filepath.Join("runs", id, "artifacts"))
		if (status == "completed") != os.IsNotExist(err) {
			t.Fatal(status, err)
		}
		if _, err := s.Run(id); err != nil {
			t.Fatal("run record lost", err)
		}
	}
}
