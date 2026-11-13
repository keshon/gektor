package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func TestArtifactStreamsOverlapAndRemainPrivateUntilPublished(t *testing.T) {
	s, base, work := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	observer, err := OpenStore(base, work)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { observer.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	entered, release := make(chan struct{}, 2), make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	t.Cleanup(func() { cancel(); workers.Wait() })
	for range 2 {
		workers.Go(func() {
			source := &gatedArtifactReader{
				done: ctx.Done(), reader: strings.NewReader("payload"), entered: entered, release: release,
			}
			_, putErr := w.Artifacts("parallel").Put(ctx, "payload.txt", source)
			results <- putErr
		})
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("artifact streams were serialized")
		}
	}
	items, err := w.ListArtifacts()
	if err != nil || len(items) != 0 {
		t.Fatal("unfinished artifact became visible", items, err)
	}
	var id string
	err = s.db.QueryRow("SELECT id FROM artifacts WHERE run_id = ? LIMIT 1", w.id).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = w.Artifacts("read").Read(t.Context(), id)
	if err == nil {
		t.Fatal("read an unpublished artifact")
	}
	err = observer.ReconcileArtifacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var count int
	err = s.db.QueryRow("SELECT count(*) FROM artifacts WHERE run_id = ?", w.id).Scan(&count)
	if err != nil || count != 2 {
		t.Fatal("recovery touched a live producer", count, err)
	}
	close(release)
	workers.Wait()
	for range 2 {
		err = <-results
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err = w.ListArtifacts()
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	for _, item := range items {
		_, err = s.root.Stat(filepath.Join(w.artifactDirectory(), item.ID, "metadata.json"))
		if !os.IsNotExist(err) {
			t.Fatal("file metadata still used", err)
		}
		_, payload, readErr := w.Artifacts("read").Read(t.Context(), item.ID)
		if readErr != nil || string(payload) != "payload" {
			t.Fatal(string(payload), readErr)
		}
	}
}

type gatedArtifactReader struct {
	done    <-chan struct{}
	reader  io.Reader
	entered chan<- struct{}
	release <-chan struct{}
	started bool
}

func (r *gatedArtifactReader) Read(p []byte) (int, error) {
	if !r.started {
		r.started = true
		r.entered <- struct{}{}
		select {
		case <-r.done:
			return 0, context.Canceled
		case <-r.release:
		}
	}
	return r.reader.Read(p)
}

func TestArtifactQuotaWaitCanBeCancelledAndCapacityReused(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var reservations []string
	for index := range MaxRunArtifactBytes / MaxArtifactBytes {
		id := fmt.Sprintf("reservation-%d", index)
		limit, reserveErr := w.reserveArtifact(t.Context(), id)
		if reserveErr != nil || limit != MaxArtifactBytes {
			t.Fatal(limit, reserveErr)
		}
		reservations = append(reservations, id)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = w.Artifacts("waiting").Put(ctx, "payload.txt", strings.NewReader("payload"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("temporary reservations did not wait for cancellation", err)
	}
	err = w.discardArtifact(reservations[0])
	if err != nil {
		t.Fatal(err)
	}
	w.artifactsChanged(nil)
	_, err = w.Artifacts("resumed").Put(t.Context(), "payload.txt", strings.NewReader("payload"))
	if err != nil {
		t.Fatal("released capacity was not reusable", err)
	}
	for _, id := range reservations[1:] {
		err = w.discardArtifact(id)
		if err != nil {
			t.Fatal(err)
		}
	}
	w.artifactsChanged(nil)
	var reserved int64
	err = s.db.QueryRow("SELECT sum(bytes) FROM artifacts WHERE run_id = ?", w.id).Scan(&reserved)
	if err != nil || reserved != int64(len("payload")) {
		t.Fatal("unused reservation bytes retained", reserved, err)
	}
}

func TestArtifactRunByteQuotaExactFit(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// Metadata-only fixtures exercise byte accounting without writing 256 MiB.
	for index := range MaxRunArtifactBytes / MaxArtifactBytes {
		size := int64(MaxArtifactBytes)
		if index == MaxRunArtifactBytes/MaxArtifactBytes-1 {
			size -= 3
		}
		id := fmt.Sprintf("existing-%d", index)
		meta := workflow.Artifact{ID: id, RunID: w.id, Name: "fixture", Size: size}
		raw, encodeErr := json.Marshal(meta)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, err = s.db.Exec("INSERT INTO artifacts VALUES (?, ?, 'published', ?, ?)", w.id, id, size, raw)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = w.Artifacts("boundary").Put(t.Context(), "fits.txt", strings.NewReader("abc"))
	if err != nil {
		t.Fatal("exact remaining quota rejected", err)
	}
	_, err = w.Artifacts("boundary").Put(t.Context(), "overflow.txt", strings.NewReader("x"))
	if err == nil {
		t.Fatal("run byte quota exceeded")
	}
	_, err = w.Artifacts("boundary").Put(t.Context(), "empty.txt", strings.NewReader(""))
	if err != nil {
		t.Fatal("empty artifact rejected at exact byte limit", err)
	}
	var used int64
	err = s.db.QueryRow("SELECT sum(bytes) FROM artifacts WHERE run_id = ?", w.id).Scan(&used)
	if err != nil || used != MaxRunArtifactBytes {
		t.Fatal("byte accounting drifted", used, err)
	}
}

func TestArtifactPublicationFailureReleasesFilesAndQuota(t *testing.T) {
	for _, action := range []string{"ABORT, 'storage fault'", "IGNORE"} {
		t.Run(action, func(t *testing.T) {
			s, _, _ := testStore(t)
			w, err := s.NewRun()
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			_, err = s.db.Exec("CREATE TRIGGER reject_publish BEFORE UPDATE ON artifacts WHEN NEW.status = 'published' BEGIN SELECT RAISE(" + action + "); END")
			if err != nil {
				t.Fatal(err)
			}
			_, err = w.Artifacts("fail").Put(t.Context(), "payload.txt", strings.NewReader("payload"))
			if err == nil {
				t.Fatal("publication failure ignored")
			}
			var count int
			err = s.db.QueryRow("SELECT count(*) FROM artifacts WHERE run_id = ?", w.id).Scan(&count)
			if err != nil || count != 0 {
				t.Fatal("reservation leaked", count, err)
			}
			dir, err := s.root.Open(w.artifactDirectory())
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			entries, err := dir.ReadDir(-1)
			if err != nil || len(entries) != 0 {
				t.Fatal("failed publication bytes retained", entries, err)
			}
		})
	}
}

func TestArtifactCleanupFailureRequiresRecovery(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	_, err = s.db.Exec("CREATE TRIGGER reject_cleanup BEFORE DELETE ON artifacts BEGIN SELECT RAISE(ABORT, 'cleanup fault'); END")
	if err != nil {
		t.Fatal(err)
	}
	source := &interruptedArtifactReader{failure: errors.New("source fault")}
	_, err = w.Artifacts("fail").Put(t.Context(), "payload.txt", source)
	if err == nil || !strings.Contains(err.Error(), "cleanup fault") {
		t.Fatal("cleanup failure hidden", err)
	}
	_, err = w.Artifacts("blocked").Put(t.Context(), "next.txt", strings.NewReader("next"))
	if err == nil || !strings.Contains(err.Error(), "requires recovery") {
		t.Fatal("writer accepted work with stranded reservations", err)
	}
	_, err = s.db.Exec("DROP TRIGGER reject_cleanup")
	if err != nil {
		t.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = s.ReconcileArtifacts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	recovered, _, err := s.EditRun(w.id)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	meta, err := recovered.Artifacts("recovered").Put(t.Context(), "next.txt", strings.NewReader("next"))
	if err != nil {
		t.Fatal(err)
	}
	err = recovered.discardArtifact(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, payload, err := recovered.Artifacts("read").Read(t.Context(), meta.ID)
	if err != nil || string(payload) != "next" {
		t.Fatal("cleanup removed a committed publication", string(payload), err)
	}
}

func TestArtifactRetentionRemovesMetadataWhenPayloadIsMissing(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	_, err = w.Artifacts("save").Put(t.Context(), "payload.txt", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	err = w.Save(workflow.Run{Status: "completed", Finished: &old})
	if err != nil {
		t.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = s.root.RemoveAll(w.artifactDirectory())
	if err != nil {
		t.Fatal(err)
	}
	removed, err := s.PruneArtifacts(t.Context(), time.Now())
	if err != nil || len(removed) != 1 {
		t.Fatal(removed, err)
	}
	var count int
	err = s.db.QueryRow("SELECT count(*) FROM artifacts WHERE run_id = ?", w.id).Scan(&count)
	if err != nil || count != 0 {
		t.Fatal("metadata remained after retention", count, err)
	}
	removed, err = s.PruneArtifacts(t.Context(), time.Now())
	if err != nil || len(removed) != 0 {
		t.Fatal("repeated retention was not idempotent", removed, err)
	}
}
