package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

const maxWaitingRuns = 1000

// A deadline and its invocation checkpoint commit together. Leaving waiting state
// removes the deadline before any resumed effects, so interrupted work is not retried.
func saveWakeup(tx *sql.Tx, id string, r workflow.Run) error {
	if r.Status != "waiting" {
		_, err := tx.Exec("DELETE FROM wakeups WHERE run_id = ?", id)
		return err
	}
	if r.WakeAt == nil || r.WakeAt.IsZero() {
		return fmt.Errorf("waiting run requires a deadline")
	}
	var count int
	err := tx.QueryRow("SELECT count(*) FROM wakeups WHERE run_id != ?", id).Scan(&count)
	if err != nil {
		return err
	}
	if count >= maxWaitingRuns {
		return fmt.Errorf("waiting run limit reached (%d)", maxWaitingRuns)
	}

	_, err = tx.Exec("INSERT INTO wakeups VALUES (?, ?) ON CONFLICT(run_id) DO UPDATE SET due=excluded.due", id, r.WakeAt.UnixMilli())
	return err
}

// ClaimWait returns a leased due run. The caller must close its writer after
// joining execution. A second worker cannot claim the same run while it is leased.
func (s *Store) ClaimWait(ctx context.Context, now time.Time) (*RunWriter, workflow.Run, bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT run_id FROM wakeups WHERE due <= ? ORDER BY due, run_id LIMIT ?", now.UnixMilli(), maxWaitingRuns)
	if err != nil {
		return nil, workflow.Run{}, false, err
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
		return nil, workflow.Run{}, false, err
	}

	for _, id := range ids {
		writer, r, claimErr := s.EditRun(id)
		if errors.Is(claimErr, errLocked) {
			continue
		}
		if claimErr != nil {
			return nil, workflow.Run{}, false, claimErr
		}
		if r.Status == "waiting" && r.WakeAt != nil && !r.WakeAt.After(now) {
			return writer, r, true, nil
		}
		err = writer.Close()
		if err != nil {
			return nil, workflow.Run{}, false, err
		}
	}
	return nil, workflow.Run{}, false, nil
}
