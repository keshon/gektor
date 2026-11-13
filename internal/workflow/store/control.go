package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
)

// Queue and schedule decisions share one immediate transaction. Only changed
// rows are written; a failed callback leaves both collections untouched.
func (s *Store) control(ctx context.Context, fn func(*control) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	c := control{tx: tx}
	err = tx.QueryRowContext(ctx, "SELECT schedule_revision FROM control WHERE id = 1").Scan(&c.ScheduleRevision)
	if err != nil {
		return err
	}
	revision := c.ScheduleRevision
	c.Jobs, err = controlRows[Job](ctx, tx, "jobs")
	if err != nil {
		return err
	}
	c.Schedules, err = controlRows[Schedule](ctx, tx, "schedules")
	if err != nil {
		return err
	}
	beforeJobs, err := encodeControlRows(c.Jobs)
	if err != nil {
		return err
	}
	beforeSchedules, err := encodeControlRows(c.Schedules)
	if err != nil {
		return err
	}
	err = fn(&c)
	if err != nil {
		return err
	}
	err = saveControlRows(ctx, tx, "jobs", beforeJobs, c.Jobs)
	if err != nil {
		return err
	}
	err = saveControlRows(ctx, tx, "schedules", beforeSchedules, c.Schedules)
	if err != nil {
		return err
	}
	if revision != c.ScheduleRevision {
		_, err = tx.ExecContext(ctx, "UPDATE control SET schedule_revision = ? WHERE id = 1", c.ScheduleRevision)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func controlRows[T any](ctx context.Context, tx *sql.Tx, table string) (map[string]T, error) {
	rows, err := tx.QueryContext(ctx, "SELECT id, record FROM "+table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]T{}
	for rows.Next() {
		var id string
		var raw []byte
		err = rows.Scan(&id, &raw)
		if err != nil {
			return nil, err
		}
		var record T
		err = json.Unmarshal(raw, &record)
		if err != nil {
			return nil, err
		}
		out[id] = record
	}
	return out, rows.Err()
}

func encodeControlRows[T any](records map[string]T) (map[string][]byte, error) {
	out := make(map[string][]byte, len(records))
	for id, record := range records {
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		out[id] = raw
	}
	return out, nil
}

// table is supplied only by the queue owner, never by an external caller.
func saveControlRows[T any](ctx context.Context, tx *sql.Tx, table string, before map[string][]byte, records map[string]T) error {
	after, err := encodeControlRows(records)
	if err != nil {
		return err
	}
	for id, raw := range after {
		if bytes.Equal(before[id], raw) {
			continue
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO "+table+" (id, record) VALUES (?, ?) ON CONFLICT(id) DO UPDATE SET record=excluded.record", id, raw)
		if err != nil {
			return err
		}
	}
	for id := range before {
		if _, exists := after[id]; exists {
			continue
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE id = ?", id)
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}
