package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/keshon/gektor/internal/workflow"
)

const maxCallbackPayload = 32 << 10
const maxCallbackBytes = 1 << 20
const maxCallbacks = 1000

var (
	ErrCallbackInvalid  = errors.New("invalid callback event")
	ErrCallbackMissing  = errors.New("callback unavailable")
	ErrCallbackClosed   = errors.New("callback closed or expired")
	ErrCallbackConflict = errors.New("callback already received a different event")
)

type CallbackDelivery struct {
	RunID   string
	WaitID  string
	Channel string
	EventID string
	Payload json.RawMessage
}

func saveCallback(tx *sql.Tx, id string, step workflow.StepResult) error {
	p := step.Callback
	var count int
	err := tx.QueryRow("SELECT count(*) FROM callbacks WHERE run_id=? AND wait_id!=?", id, p.ID).Scan(&count)
	if err != nil {
		return err
	}
	if count >= maxCallbacks {
		return fmt.Errorf("callback limit reached (%d)", maxCallbacks)
	}

	_, err = tx.Exec(`INSERT INTO callbacks(run_id,wait_id,step_id,channel,expires,state)
VALUES(?,?,?,?,?,'pending') ON CONFLICT(run_id,wait_id) DO NOTHING`, id, p.ID, p.StepID, p.Channel, p.ExpiresAt.UnixMilli())
	if err != nil {
		return err
	}
	var stepID, channel string
	var expires int64
	err = tx.QueryRow("SELECT step_id,channel,expires FROM callbacks WHERE run_id=? AND wait_id=?", id, p.ID).Scan(&stepID, &channel, &expires)
	if err != nil {
		return err
	}
	if stepID != p.StepID || channel != p.Channel || expires != p.ExpiresAt.UnixMilli() {
		return fmt.Errorf("callback binding changed")
	}

	if step.Status == "completed" {
		_, err = tx.Exec("UPDATE callbacks SET state='consumed' WHERE run_id=? AND wait_id=? AND state='accepted'", id, p.ID)
	}
	return err
}

func loadCallback(tx *sql.Tx, id string, p *workflow.CallbackState) error {
	var state string
	var payload []byte
	err := tx.QueryRow("SELECT state,event_id,payload FROM callbacks WHERE run_id=? AND wait_id=?", id, p.ID).Scan(&state, &p.EventID, &payload)
	p.Payload = payload
	if state != "accepted" {
		p.EventID, p.Payload = "", nil
	}
	return err
}

// AcceptCallback commits the event and wake-up together. Duplicate acknowledgement
// lasts as long as the run is retained; it never schedules another continuation.
func (s *Store) AcceptCallback(ctx context.Context, delivery CallbackDelivery) (bool, error) {
	if delivery.EventID == "" || len(delivery.EventID) > 128 || strings.TrimSpace(delivery.EventID) != delivery.EventID || strings.ContainsFunc(delivery.EventID, unicode.IsControl) || len(delivery.Payload) > maxCallbackPayload {
		return false, ErrCallbackInvalid
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(delivery.Payload))
	decoder.UseNumber()
	err := decoder.Decode(&value)
	if err != nil {
		return false, ErrCallbackInvalid
	}
	err = decoder.Decode(new(any))
	if err != io.EOF {
		return false, ErrCallbackInvalid
	}
	payload, err := json.Marshal(value)
	if err != nil || len(payload) > maxCallbackPayload {
		return false, ErrCallbackInvalid
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var channel, state, eventID, status string
	var previous []byte
	var expires int64
	err = tx.QueryRowContext(ctx, `SELECT c.channel,c.state,c.event_id,c.payload,c.expires,r.status
FROM callbacks c JOIN runs r ON r.id=c.run_id WHERE c.run_id=? AND c.wait_id=?`, delivery.RunID, delivery.WaitID).Scan(&channel, &state, &eventID, &previous, &expires, &status)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && channel != delivery.Channel) {
		return false, ErrCallbackMissing
	}
	if err != nil {
		return false, err
	}
	if eventID != "" {
		if eventID != delivery.EventID || !bytes.Equal(previous, payload) {
			return false, ErrCallbackConflict
		}
		return true, tx.Commit()
	}
	if state != "pending" || status != "waiting" || time.Now().UnixMilli() >= expires {
		return false, ErrCallbackClosed
	}

	// Cancellation is stored in the queue record in the same database transaction.
	rows, err := tx.QueryContext(ctx, "SELECT record FROM jobs")
	if err != nil {
		return false, err
	}
	cancelled := false
	for rows.Next() {
		var raw []byte
		err = rows.Scan(&raw)
		if err != nil {
			break
		}
		var job Job
		err = json.Unmarshal(raw, &job)
		if err != nil {
			break
		}
		cancelled = cancelled || (job.RunID == delivery.RunID && job.Cancel)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return false, err
	}
	if cancelled {
		return false, ErrCallbackClosed
	}
	var total int
	err = tx.QueryRowContext(ctx, "SELECT coalesce(sum(length(payload)),0) FROM callbacks WHERE run_id=?", delivery.RunID).Scan(&total)
	if err != nil {
		return false, err
	}
	if total+len(payload) > maxCallbackBytes {
		return false, ErrCallbackInvalid
	}

	_, err = tx.ExecContext(ctx, "UPDATE callbacks SET state='accepted',event_id=?,payload=? WHERE run_id=? AND wait_id=?", delivery.EventID, payload, delivery.RunID, delivery.WaitID)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE wakeups SET due=? WHERE run_id=?", time.Now().UnixMilli(), delivery.RunID)
	if err != nil {
		return false, err
	}
	return false, tx.Commit()
}
