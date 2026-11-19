package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Status contains one database snapshot and a separately sampled worker lease.
// Counts describe retained records, not lifetime totals or successful effects.
type Status struct {
	ObservedAt      time.Time                `json:"observed_at"`
	WorkerLeaseHeld bool                     `json:"worker_lease_held"`
	LastWorkerTick  *time.Time               `json:"last_worker_tick"`
	Jobs            map[string]int64         `json:"jobs"`
	OldestQueued    *time.Time               `json:"oldest_queued"`
	Runs            map[string]int64         `json:"runs"`
	Schedules       int64                    `json:"schedules"`
	PausedSchedules int64                    `json:"paused_schedules"`
	DueWaits        int64                    `json:"due_waits"`
	Artifacts       map[string]ArtifactUsage `json:"artifacts"`
}

type ArtifactUsage struct {
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`
}

// SaveWorkerTick records scheduler progress under the caller's daemon lease.
// Zero clears the previous worker's tick before startup reconciliation.
func (s *Store) SaveWorkerTick(ctx context.Context, at time.Time) error {
	var tick sql.NullString
	if !at.IsZero() {
		tick.String = at.UTC().Format(time.RFC3339Nano)
		tick.Valid = true
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO worker VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET last_tick=excluded.last_tick", tick)
	return err
}

// Status reads metadata without hydrating recipe, input, invocation or artifact
// payloads. A recent tick describes the scheduler, not operation health.
func (s *Store) Status(ctx context.Context) (Status, error) {
	var status Status
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return status, err
	}
	defer tx.Rollback() // No observations should change saved state.
	status.ObservedAt = time.Now().UTC()

	status.Jobs, err = statusCounts(ctx, tx, `SELECT json_extract(CAST(record AS TEXT), '$.status'), count(*) FROM jobs GROUP BY 1`)
	if err != nil {
		return Status{}, err
	}
	status.Runs, err = statusCounts(ctx, tx, "SELECT status, count(*) FROM runs GROUP BY status")
	if err != nil {
		return Status{}, err
	}
	status.OldestQueued, err = oldestQueued(ctx, tx)
	if err != nil {
		return Status{}, err
	}

	row := tx.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(json_extract(CAST(record AS TEXT), '$.paused')), 0) FROM schedules`)
	err = row.Scan(&status.Schedules, &status.PausedSchedules)
	if err != nil {
		return Status{}, err
	}
	row = tx.QueryRowContext(ctx, "SELECT count(*) FROM wakeups WHERE due <= ?", status.ObservedAt.UnixMilli())
	err = row.Scan(&status.DueWaits)
	if err != nil {
		return Status{}, err
	}
	status.Artifacts, err = artifactUsage(ctx, tx)
	if err != nil {
		return Status{}, err
	}

	var tick sql.NullString
	row = tx.QueryRowContext(ctx, "SELECT last_tick FROM worker WHERE id = 1")
	err = row.Scan(&tick)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Status{}, err
	}
	if tick.Valid {
		var at time.Time
		at, err = time.Parse(time.RFC3339Nano, tick.String)
		if err != nil {
			return Status{}, err
		}
		status.LastWorkerTick = &at
	}
	err = tx.Commit()
	if err != nil {
		return Status{}, err
	}

	status.WorkerLeaseHeld, err = s.WorkerLeaseHeld()
	if err != nil {
		return Status{}, err
	}
	return status, nil
}

func statusCounts(ctx context.Context, tx *sql.Tx, query string) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for rows.Next() {
		var status string
		var count int64
		err = rows.Scan(&status, &count)
		if err != nil {
			break
		}
		counts[status] = count
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	return counts, errors.Join(err, rowsErr, closeErr)
}

func oldestQueued(ctx context.Context, tx *sql.Tx) (*time.Time, error) {
	rows, err := tx.QueryContext(ctx, `SELECT json_extract(CAST(record AS TEXT), '$.created') FROM jobs WHERE json_extract(CAST(record AS TEXT), '$.status')='queued'`)
	if err != nil {
		return nil, err
	}
	var oldest *time.Time
	for rows.Next() {
		var created string
		err = rows.Scan(&created)
		if err != nil {
			break
		}
		var at time.Time
		at, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			break
		}
		// RFC3339 offsets and variable fractions do not sort chronologically as text.
		if oldest == nil || at.Before(*oldest) {
			oldest = &at
		}
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	return oldest, errors.Join(err, rowsErr, closeErr)
}

func artifactUsage(ctx context.Context, tx *sql.Tx) (map[string]ArtifactUsage, error) {
	rows, err := tx.QueryContext(ctx, "SELECT status, count(*), sum(bytes) FROM artifacts GROUP BY status")
	if err != nil {
		return nil, err
	}
	usage := map[string]ArtifactUsage{}
	for rows.Next() {
		var status string
		var value ArtifactUsage
		err = rows.Scan(&status, &value.Count, &value.Bytes)
		if err != nil {
			break
		}
		usage[status] = value
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	return usage, errors.Join(err, rowsErr, closeErr)
}
