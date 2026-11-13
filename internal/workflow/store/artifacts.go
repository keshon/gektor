package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

const MaxArtifactBytes = 16 << 20
const MaxRunArtifactBytes = 256 << 20
const MaxRunArtifacts = 256

type artifactAccess struct {
	writer *RunWriter
	step   string
}

func (w *RunWriter) Artifacts(step string) workflow.ArtifactAccess {
	return artifactAccess{w, step}
}

func (w *RunWriter) artifactDirectory() string { return filepath.Join("runs", w.id, "artifacts") }

func (w *RunWriter) ListArtifacts() ([]workflow.Artifact, error) {
	if _, err := w.lease.Stat(); err != nil {
		return nil, fmt.Errorf("run writer closed: %w", err)
	}
	f, err := w.store.root.Open(w.artifactDirectory())
	if os.IsNotExist(err) {
		return []workflow.Artifact{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	out := []workflow.Artifact{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			continue
		}
		if !e.IsDir() || !validKey(e.Name()) {
			return nil, fmt.Errorf("invalid artifact entry")
		}
		a, err := w.artifactMetadata(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (w *RunWriter) artifactMetadata(id string) (workflow.Artifact, error) {
	var a workflow.Artifact
	if !validKey(id) {
		return a, fmt.Errorf("invalid artifact ID")
	}
	err := w.store.read(filepath.Join(w.artifactDirectory(), id, "metadata.json"), 8192, &a)
	if os.IsNotExist(err) {
		return a, fmt.Errorf("artifact %q not found in run %s (it may have been pruned)", id, w.id)
	}
	if err != nil {
		return a, err
	}
	if a.ID != id || a.RunID != w.id || a.Size < 0 || a.Size > MaxArtifactBytes {
		return a, fmt.Errorf("artifact identity or size mismatch")
	}
	return a, nil
}

func (a artifactAccess) Put(ctx context.Context, name string, source io.Reader) (workflow.Artifact, error) {
	w := a.writer
	w.artifactMu.Lock()
	defer w.artifactMu.Unlock()

	var zero workflow.Artifact
	if name == "" || len(name) > 255 || strings.ContainsAny(name, "/\\\x00\r\n") || name == "." || name == ".." {
		return zero, fmt.Errorf("artifact name must be a filename of at most 255 bytes")
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	entries, err := w.ListArtifacts()
	if err != nil {
		return zero, err
	}
	if len(entries) >= MaxRunArtifacts {
		return zero, fmt.Errorf("run artifact count exceeds %d", MaxRunArtifacts)
	}
	var used int64
	for _, entry := range entries {
		used += entry.Size
	}
	if err := w.store.root.MkdirAll(w.artifactDirectory(), 0700); err != nil {
		return zero, err
	}
	id := rand.Text()
	tmp := filepath.Join(w.artifactDirectory(), ".tmp-"+id)
	if err := w.store.root.Mkdir(tmp, 0700); err != nil {
		return zero, err
	}
	defer w.store.root.RemoveAll(tmp)
	f, err := w.store.root.OpenFile(filepath.Join(tmp, "data"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return zero, err
	}
	hash := sha256.New()
	// Bounded buffering also lets us detect a useful media type without trusting a suffix.
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, source}, MaxArtifactBytes+1))
	if err == nil && (len(data) > MaxArtifactBytes || used+int64(len(data)) > MaxRunArtifactBytes) {
		err = fmt.Errorf("artifact exceeds 16 MiB or run exceeds 256 MiB")
	}
	if err == nil {
		_, err = io.MultiWriter(f, hash).Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return zero, err
	}
	if closeErr != nil {
		return zero, closeErr
	}
	meta := workflow.Artifact{ID: id, RunID: w.id, StepID: a.step, Name: name, MediaType: http.DetectContentType(data), Size: int64(len(data)), SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Created: time.Now().UTC()}
	if err := w.store.write(filepath.Join(tmp, "metadata.json"), meta); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := w.store.root.Rename(tmp, filepath.Join(w.artifactDirectory(), id)); err != nil {
		return zero, err
	}
	return meta, nil
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func (a artifactAccess) Read(ctx context.Context, id string) (workflow.Artifact, []byte, error) {
	w := a.writer
	if _, err := w.lease.Stat(); err != nil {
		return workflow.Artifact{}, nil, err
	}
	meta, err := w.artifactMetadata(id)
	if err != nil {
		return meta, nil, err
	}
	f, err := w.store.root.Open(filepath.Join(w.artifactDirectory(), id, "data"))
	if err != nil {
		return meta, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return meta, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != meta.Size {
		return meta, nil, fmt.Errorf("artifact size mismatch")
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, f}, MaxArtifactBytes+1))
	if err != nil {
		return meta, nil, err
	}
	if int64(len(data)) != meta.Size || fmt.Sprintf("%x", sha256.Sum256(data)) != meta.SHA256 {
		return meta, nil, fmt.Errorf("artifact integrity check failed")
	}
	return meta, data, nil
}

// PruneArtifacts uses each run lease, preserving active and reviewable runs.
// Run records remain as evidence after payload retention expires.
func (s *Store) PruneArtifacts(ctx context.Context, before time.Time) ([]string, error) {
	runs, err := s.Runs()
	if err != nil {
		return nil, err
	}
	removed := []string{}
	for _, run := range runs {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if run.Status != "completed" && run.Status != "failed" && run.Status != "cancelled" {
			continue
		}
		w, record, err := s.EditRun(run.ID)
		if err == errLocked {
			continue
		}
		if err != nil {
			return removed, err
		}
		err = func() error {
			defer w.Close()
			if record.Status != "completed" && record.Status != "failed" && record.Status != "cancelled" {
				return nil
			}
			if record.Finished == nil || !record.Finished.Before(before) {
				return nil
			}
			if _, err := s.root.Stat(w.artifactDirectory()); os.IsNotExist(err) {
				return nil
			} else if err != nil {
				return err
			}
			if err := s.root.RemoveAll(w.artifactDirectory()); err != nil {
				return err
			}
			removed = append(removed, run.ID)
			return nil
		}()
		if err != nil {
			return removed, err
		}
	}
	return removed, nil
}
