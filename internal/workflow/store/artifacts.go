package store

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	_, err := w.lease.Stat()
	if err != nil {
		return nil, fmt.Errorf("run writer closed: %w", err)
	}
	rows, err := w.store.db.Query("SELECT id, record FROM artifacts WHERE run_id = ? AND status = 'published' ORDER BY id", w.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []workflow.Artifact{}
	for rows.Next() {
		var id string
		var raw []byte
		err = rows.Scan(&id, &raw)
		if err != nil {
			return nil, err
		}
		var meta workflow.Artifact
		err = json.Unmarshal(raw, &meta)
		if err != nil {
			return nil, err
		}
		if !validKey(id) || meta.ID != id || meta.RunID != w.id || meta.Size < 0 || meta.Size > MaxArtifactBytes {
			return nil, fmt.Errorf("artifact identity or size mismatch")
		}
		out = append(out, meta)
	}
	return out, rows.Err()
}

func (w *RunWriter) artifactMetadata(id string) (workflow.Artifact, error) {
	var meta workflow.Artifact
	if !validKey(id) {
		return meta, fmt.Errorf("invalid artifact ID")
	}
	var raw []byte
	err := w.store.db.QueryRow("SELECT record FROM artifacts WHERE run_id = ? AND id = ? AND status = 'published'", w.id, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return meta, fmt.Errorf("artifact %q not found in run %s (it may have been pruned)", id, w.id)
	}
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(raw, &meta)
	if err != nil {
		return meta, err
	}
	if meta.ID != id || meta.RunID != w.id || meta.Size < 0 || meta.Size > MaxArtifactBytes {
		return meta, fmt.Errorf("artifact identity or size mismatch")
	}
	return meta, nil
}

func (a artifactAccess) Put(ctx context.Context, name string, source io.Reader) (artifact workflow.Artifact, err error) {
	w := a.writer

	var zero workflow.Artifact
	if name == "" || len(name) > 255 || strings.ContainsAny(name, "/\\\x00\r\n") || name == "." || name == ".." {
		return zero, fmt.Errorf("artifact name must be a filename of at most 255 bytes")
	}
	err = ctx.Err()
	if err != nil {
		return zero, err
	}
	id := rand.Text()
	defer func() {
		var cleanupErr error
		if err != nil {
			cleanupErr = w.discardArtifact(id)
		}
		w.artifactsChanged(cleanupErr)
		err = errors.Join(err, cleanupErr)
	}()
	limit, err := w.reserveArtifact(ctx, id)
	if err != nil {
		return zero, err
	}

	err = w.store.root.MkdirAll(filepath.Join(w.artifactDirectory(), id), 0700)
	if err != nil {
		return zero, err
	}

	reader := bufio.NewReader(contextReader{ctx, source})
	prefix, err := reader.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) {
		return zero, err
	}
	mediaType := http.DetectContentType(prefix)
	f, err := w.store.root.OpenFile(filepath.Join(w.artifactDirectory(), id, "data"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return zero, err
	}

	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(reader, limit))
	if err == nil {
		// Probe past the limit without writing beyond the reserved bytes.
		var extra [1]byte
		n, readErr := io.ReadFull(reader, extra[:])
		if n > 0 {
			err = fmt.Errorf("artifact exceeds 16 MiB or run exceeds 256 MiB")
		} else if readErr != nil && !errors.Is(readErr, io.EOF) {
			err = readErr
		}
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
	meta := workflow.Artifact{
		ID:        id,
		RunID:     w.id,
		StepID:    a.step,
		Name:      name,
		MediaType: mediaType,
		Size:      size,
		SHA256:    fmt.Sprintf("%x", hash.Sum(nil)),
		Created:   time.Now().UTC(),
	}
	err = ctx.Err()
	if err != nil {
		return zero, err
	}
	_, err = w.lease.Stat()
	if err != nil {
		return zero, err
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return zero, err
	}
	result, err := w.store.db.ExecContext(ctx, "UPDATE artifacts SET status = 'published', bytes = ?, record = ? WHERE run_id = ? AND id = ? AND status = 'writing' AND bytes >= ?", size, raw, w.id, id, size)
	if err != nil {
		return zero, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return zero, err
	}
	if affected != 1 {
		return zero, fmt.Errorf("artifact reservation lost before publication")
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
			_, err = s.root.Stat(w.artifactDirectory())
			missing := os.IsNotExist(err)
			if err != nil && !missing {
				return err
			}
			var count int
			err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM artifacts WHERE run_id = ?", w.id).Scan(&count)
			if err != nil {
				return err
			}
			if missing && count == 0 {
				return nil
			}
			_, err = s.db.ExecContext(ctx, "UPDATE artifacts SET status = 'deleting' WHERE run_id = ?", w.id)
			if err != nil {
				return err
			}
			err = s.root.RemoveAll(w.artifactDirectory())
			if err != nil {
				return err
			}
			_, err = s.db.ExecContext(ctx, "DELETE FROM artifacts WHERE run_id = ?", w.id)
			if err != nil {
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
