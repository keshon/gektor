package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

func TestConcurrentArtifactPublicationHonorsRunQuota(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var workers sync.WaitGroup
	var published atomic.Int32
	for worker := range 8 {
		workers.Go(func() {
			for n := worker; n < MaxRunArtifacts+8; n += 8 {
				_, err := w.Artifacts("parallel").Put(t.Context(), "item.txt", strings.NewReader("item"))
				if err == nil {
					published.Add(1)
				} else if !strings.Contains(err.Error(), "artifact count") {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
	items, err := w.ListArtifacts()
	if err != nil || published.Load() != MaxRunArtifacts || len(items) != MaxRunArtifacts {
		t.Fatal("quota exceeded or publication lost", published.Load(), len(items), err)
	}
}

type ioZeros struct{}

func (ioZeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestArtifactStreamingPreservesBytesAndMetadata(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	payload := bytes.Repeat([]byte("plain text payload\n"), 65536)
	source := boundedArtifactReader{Reader: bytes.NewReader(payload)}
	meta, err := w.Artifacts("stream").Put(t.Context(), "payload.txt", source)
	if err != nil {
		t.Fatal(err)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256(payload))
	if meta.Size != int64(len(payload)) || meta.SHA256 != wantHash || meta.MediaType != "text/plain; charset=utf-8" {
		t.Fatal(meta)
	}
	_, actual, err := w.Artifacts("read").Read(t.Context(), meta.ID)
	if err != nil || !bytes.Equal(payload, actual) {
		t.Fatal("streamed bytes differ", err)
	}
}

func TestArtifactStreamingSizeBoundaries(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for _, size := range []int64{0, MaxArtifactBytes} {
		source := io.LimitReader(ioZeros{}, size)
		meta, err := w.Artifacts("boundary").Put(t.Context(), "bytes.bin", source)
		if err != nil || meta.Size != size {
			t.Fatal("exact size rejected", size, meta.Size, err)
		}
	}
	_, err = w.Artifacts("boundary").Put(t.Context(), "too-large.bin", io.LimitReader(ioZeros{}, MaxArtifactBytes+1))
	if err == nil {
		t.Fatal("one byte beyond the size limit accepted")
	}
	items, err := w.ListArtifacts()
	if err != nil || len(items) != 2 {
		t.Fatal("failed publication changed artifacts", items, err)
	}
}

// Reject growing read buffers: publication must use bounded scratch space.
type boundedArtifactReader struct {
	io.Reader
}

func (r boundedArtifactReader) Read(p []byte) (int, error) {
	if len(p) > 32<<10 {
		return 0, fmt.Errorf("read buffer grew to %d bytes", len(p))
	}
	return r.Reader.Read(p)
}

func TestArtifactFailedStreamIsNeverPublished(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancel), func(t *testing.T) {
			s, _, _ := testStore(t)
			w, err := s.NewRun()
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()

			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			failure := errors.New("source interrupted")
			source := &interruptedArtifactReader{failure: failure}
			if cancel {
				source.stop = stop
				failure = context.Canceled
			}
			_, err = w.Artifacts("stream").Put(ctx, "partial.txt", source)
			if !errors.Is(err, failure) {
				t.Fatalf("wanted %v, got %v", failure, err)
			}
			dir, err := s.root.Open(w.artifactDirectory())
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			entries, err := dir.ReadDir(-1)
			if err != nil || len(entries) != 0 {
				t.Fatal("partial publication remains", entries, err)
			}
		})
	}
}

type interruptedArtifactReader struct {
	reads   int
	failure error
	stop    context.CancelFunc
}

func (r *interruptedArtifactReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads == 3 {
		if r.stop != nil {
			r.stop()
			return 0, nil
		}
		return 0, r.failure
	}
	clear(p)
	return len(p), nil
}

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
