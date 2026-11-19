package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PruneRuns removes terminal history and its linked terminal jobs. Active,
// waiting, reviewable and interrupted runs are retained. Callers choose the age;
// nothing is removed automatically. Persistent lease files are kept.
func (s *Store) PruneRuns(ctx context.Context, before time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM runs WHERE status IN ('completed', 'failed', 'cancelled') ORDER BY id")
	if err != nil {
		return nil, err
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
	rowsErr := rows.Err()
	closeErr := rows.Close()
	err = errors.Join(err, rowsErr, closeErr)
	if err != nil {
		return nil, err
	}

	removed := []string{}
	for _, id := range ids {
		var pruned bool
		pruned, err = s.pruneRun(ctx, id, before)
		if pruned {
			removed = append(removed, id)
		}
		if err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func (s *Store) pruneRun(ctx context.Context, id string, before time.Time) (pruned bool, resultErr error) {
	err := ctx.Err()
	if err != nil {
		return false, err
	}
	if !validKey(id) {
		return false, fmt.Errorf("invalid run ID")
	}
	lease, err := s.lock(filepath.Join("runs", id, "lock"))
	if errors.Is(err, errLocked) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()

	var status string
	var raw []byte
	row := s.db.QueryRowContext(ctx, "SELECT status, finished FROM runs WHERE id = ?", id)
	err = row.Scan(&status, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // Another pruner finished before this lease was acquired.
	}
	if err != nil {
		return false, err
	}
	if status != "completed" && status != "failed" && status != "cancelled" {
		return false, nil
	}
	var finished *time.Time
	err = json.Unmarshal(raw, &finished)
	if err != nil {
		return false, err
	}
	if finished == nil || !finished.Before(before) {
		return false, nil
	}

	// A job may still be committing its outcome after the run finishes.
	var pending int
	row = s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs
WHERE json_extract(CAST(record AS TEXT), '$.run_id') = ?
AND coalesce(json_extract(CAST(record AS TEXT), '$.status'), '') NOT IN ('completed', 'failed', 'cancelled')`, id)
	err = row.Scan(&pending)
	if err != nil || pending > 0 {
		return false, err
	}
	writer := &RunWriter{store: s, id: id, lease: lease}
	_, err = writer.pruneArtifacts(ctx)
	if err != nil {
		return false, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() // The run and linked job history disappear together.
	// Job state has its own writer; recheck it after filesystem cleanup.
	row = tx.QueryRowContext(ctx, `SELECT count(*) FROM jobs
WHERE json_extract(CAST(record AS TEXT), '$.run_id') = ?
AND coalesce(json_extract(CAST(record AS TEXT), '$.status'), '') NOT IN ('completed', 'failed', 'cancelled')`, id)
	err = row.Scan(&pending)
	if err != nil || pending > 0 {
		return false, err
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM jobs WHERE json_extract(CAST(record AS TEXT), '$.run_id') = ?", id)
	if err != nil {
		return false, err
	}
	deletion, err := tx.ExecContext(ctx, `DELETE FROM runs WHERE id = ? AND NOT EXISTS (
SELECT 1 FROM jobs WHERE json_extract(CAST(record AS TEXT), '$.run_id') = ?)`, id, id)
	if err != nil {
		return false, err
	}
	deleted, err := deletion.RowsAffected()
	if err != nil {
		return false, err
	}
	if deleted != 1 {
		return false, fmt.Errorf("run history deletion was not recorded")
	}
	err = tx.Commit()
	return err == nil, err
}

// The caller holds the run lease and has checked retention eligibility. File
// deletion precedes releasing quota; its durable intent survives interruption.
func (w *RunWriter) pruneArtifacts(ctx context.Context) (bool, error) {
	s := w.store
	_, err := s.root.Stat(w.artifactDirectory())
	missing := os.IsNotExist(err)
	if err != nil && !missing {
		return false, err
	}
	var count int
	row := s.db.QueryRowContext(ctx, "SELECT count(*) FROM artifacts WHERE run_id = ?", w.id)
	err = row.Scan(&count)
	if err != nil {
		return false, err
	}
	if missing && count == 0 {
		return false, nil
	}
	_, err = s.db.ExecContext(ctx, "UPDATE artifacts SET status = 'deleting' WHERE run_id = ?", w.id)
	if err != nil {
		return false, err
	}
	err = s.root.RemoveAll(w.artifactDirectory())
	if err != nil {
		return false, err
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM artifacts WHERE run_id = ?", w.id)
	return err == nil, err
}
