package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
)

func (w *RunWriter) artifactNotification() (<-chan struct{}, error) {
	w.artifactMu.Lock()
	defer w.artifactMu.Unlock()
	if w.artifactWake == nil {
		w.artifactWake = make(chan struct{})
	}
	return w.artifactWake, w.artifactErr
}

func (w *RunWriter) artifactsChanged(err error) {
	w.artifactMu.Lock()
	defer w.artifactMu.Unlock()
	if w.artifactErr == nil {
		w.artifactErr = err
	}
	if w.artifactWake != nil {
		close(w.artifactWake)
	}
	w.artifactWake = make(chan struct{})
}

func (w *RunWriter) reserveArtifact(ctx context.Context, id string) (int64, error) {
	for {
		err := ctx.Err()
		if err != nil {
			return 0, err
		}
		_, err = w.lease.Stat()
		if err != nil {
			return 0, fmt.Errorf("run writer closed: %w", err)
		}
		// Subscribe before examining the quota so a completion cannot be missed.
		changed, err := w.artifactNotification()
		if err != nil {
			return 0, fmt.Errorf("artifact cleanup requires recovery: %w", err)
		}
		limit, reserved, err := w.tryReserveArtifact(ctx, id)
		if err != nil || reserved {
			return limit, err
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-changed:
		}
	}
}

func (w *RunWriter) tryReserveArtifact(ctx context.Context, id string) (int64, bool, error) {
	tx, err := w.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	var count, writing int
	var used int64
	err = tx.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(bytes), 0),
coalesce(sum(status = 'writing'), 0) FROM artifacts WHERE run_id = ?`, w.id).Scan(&count, &used, &writing)
	if err != nil {
		return 0, false, err
	}
	if count >= MaxRunArtifacts {
		if writing > 0 {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("run artifact count exceeds %d", MaxRunArtifacts)
	}
	available := int64(MaxRunArtifactBytes) - used
	if available < 0 {
		return 0, false, fmt.Errorf("run artifact bytes exceed %d", MaxRunArtifactBytes)
	}
	if available < MaxArtifactBytes && writing > 0 {
		return 0, false, nil
	}
	limit := min(int64(MaxArtifactBytes), available)
	_, err = tx.ExecContext(ctx, "INSERT INTO artifacts (run_id, id, status, bytes) VALUES (?, ?, 'writing', ?)", w.id, id, limit)
	if err != nil {
		return 0, false, err
	}
	err = tx.Commit()
	return limit, err == nil, err
}

// A failed publish may have committed before returning an error. Inspect its
// current state before removing bytes; published artifacts are always retained.
func (w *RunWriter) discardArtifact(id string) error {
	if !validKey(id) {
		return fmt.Errorf("invalid artifact ID")
	}
	var status string
	err := w.store.db.QueryRow("SELECT status FROM artifacts WHERE run_id = ? AND id = ?", w.id, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status == "published" {
		return nil
	}
	err = w.store.root.RemoveAll(filepath.Join(w.artifactDirectory(), id))
	if err != nil {
		return err
	}
	_, err = w.store.db.Exec("DELETE FROM artifacts WHERE run_id = ? AND id = ? AND status != 'published'", w.id, id)
	return err
}

// The caller holds the exclusive run lease. No live producer can own these
// reservations, and file removal precedes releasing their byte quota.
func (w *RunWriter) reconcileArtifacts() error {
	rows, err := w.store.db.Query("SELECT id, status FROM artifacts WHERE run_id = ? AND status != 'published'", w.id)
	if err != nil {
		return err
	}
	var ids []string
	deleting := false
	for rows.Next() {
		var id, status string
		err = rows.Scan(&id, &status)
		if err != nil {
			break
		}
		ids = append(ids, id)
		deleting = deleting || status == "deleting"
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	if deleting {
		var published int
		err = w.store.db.QueryRow("SELECT count(*) FROM artifacts WHERE run_id = ? AND status = 'published'", w.id).Scan(&published)
		if err != nil {
			return err
		}
		if published == 0 {
			err = w.store.root.RemoveAll(w.artifactDirectory())
			if err != nil {
				return err
			}
			_, err = w.store.db.Exec("DELETE FROM artifacts WHERE run_id = ? AND status != 'published'", w.id)
			return err
		}
	}
	for _, id := range ids {
		err = w.discardArtifact(id)
		if err != nil {
			return err
		}
	}
	return nil
}

// ReconcileArtifacts reclaims unfinished publications and interrupted retention.
// A live run's lease excludes it even if another process calls this method.
func (s *Store) ReconcileArtifacts(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT run_id FROM artifacts WHERE status != 'published'")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		err = rows.Scan(&id)
		if err != nil {
			break
		}
		ids = append(ids, id)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	for _, id := range ids {
		err = ctx.Err()
		if err != nil {
			return err
		}
		var writer *RunWriter
		writer, _, err = s.EditRun(id)
		if errors.Is(err, errLocked) {
			continue
		}
		if err != nil {
			return err
		}
		err = writer.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
