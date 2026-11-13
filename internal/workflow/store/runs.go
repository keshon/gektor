package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/keshon/gektor/internal/workflow"
)

// Save checkpoints the whole run when path is empty. A one-element path selects
// a step; a second element selects an iteration. The engine owns these indexes.
// The writer's process lease excludes other writers for this run.
func (w *RunWriter) Save(r workflow.Run, path ...int) error {
	_, err := w.lease.Stat()
	if err != nil {
		return fmt.Errorf("run writer closed: %w", err)
	}
	if len(path) > 2 {
		return fmt.Errorf("invalid checkpoint path")
	}
	steps := r.Steps
	for _, index := range path {
		if index < 0 || index >= len(steps) {
			return fmt.Errorf("checkpoint index out of range")
		}
		steps = steps[index].Iterations
	}

	finished, err := json.Marshal(r.Finished)
	if err != nil {
		return err
	}
	tx, err := w.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if len(path) == 0 {
		r.RunID, r.Workspace = w.id, w.store.workspace
		r.Steps = nil
		record, encodeErr := json.Marshal(r)
		if encodeErr != nil {
			return encodeErr
		}
		_, err = tx.Exec(`INSERT INTO runs (id, status, finished, record) VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET status=excluded.status, finished=excluded.finished, record=excluded.record`, w.id, r.Status, finished, record)
		if err != nil {
			return err
		}
		_, err = tx.Exec("DELETE FROM invocations WHERE run_id = ?", w.id)
		if err != nil {
			return err
		}
		for index, step := range steps {
			err = saveInvocation(tx, w.id, "", index, step)
			if err != nil {
				return err
			}
		}
		return tx.Commit()
	}

	_, err = tx.Exec("UPDATE runs SET status = ?, finished = ? WHERE id = ?", r.Status, finished, w.id)
	if err != nil {
		return err
	}
	parent := ""
	step := r.Steps[path[0]]
	ordinal := path[0]
	if len(path) == 2 {
		parent = strconv.Itoa(path[0])
		ordinal = path[1]
		step = step.Iterations[ordinal]
	}
	position := invocationPosition(parent, ordinal)
	_, err = tx.Exec("DELETE FROM invocations WHERE run_id = ? AND position = ?", w.id, position)
	if err != nil {
		return err
	}
	if len(path) == 1 {
		// Numeric positions contain no wildcard characters. This byte range
		// covers descendants and uses the primary key instead of scanning a map.
		_, err = tx.Exec("DELETE FROM invocations WHERE run_id = ? AND position >= ? AND position < ?", w.id, position+"/", position+"0")
		if err != nil {
			return err
		}
	}
	err = saveInvocation(tx, w.id, parent, ordinal, step)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func invocationPosition(parent string, ordinal int) string {
	position := strconv.Itoa(ordinal)
	if parent != "" {
		position = parent + "/" + position
	}
	return position
}

func saveInvocation(tx *sql.Tx, id, parent string, ordinal int, step workflow.StepResult) error {
	children, attempts := step.Iterations, step.Attempts
	step.Iterations, step.Attempts = nil, nil
	record, err := json.Marshal(step)
	if err != nil {
		return err
	}
	position := invocationPosition(parent, ordinal)
	_, err = tx.Exec("INSERT INTO invocations VALUES (?, ?, ?, ?, ?)", id, position, parent, ordinal, record)
	if err != nil {
		return err
	}
	for _, attempt := range attempts {
		record, err = json.Marshal(attempt)
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO attempts VALUES (?, ?, ?, ?)", id, position, attempt.Number, record)
		if err != nil {
			return err
		}
	}
	for index, child := range children {
		err = saveInvocation(tx, id, position, index, child)
		if err != nil {
			return err
		}
	}
	return nil
}

func runMetadata(db *sql.Tx, id string) (workflow.Run, error) {
	var r workflow.Run
	var record, finished []byte
	var status string
	err := db.QueryRow("SELECT status, finished, record FROM runs WHERE id = ?", id).Scan(&status, &finished, &record)
	if errors.Is(err, sql.ErrNoRows) {
		return r, missingRun(id)
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(record, &r)
	if err != nil {
		return r, err
	}
	if r.RunID != id {
		return r, fmt.Errorf("run identity mismatch")
	}
	r.Status = status
	err = json.Unmarshal(finished, &r.Finished)
	if err != nil {
		return r, err
	}

	return r, nil
}

func loadRun(db *sql.Tx, id string) (workflow.Run, error) {
	r, err := runMetadata(db, id)
	if err != nil {
		return r, err
	}

	rows, err := db.Query("SELECT position, record FROM invocations WHERE run_id = ? ORDER BY length(position) DESC", id)
	if err != nil {
		return r, err
	}
	steps := map[string]*workflow.StepResult{}
	var positions []string
	for rows.Next() {
		var position string
		var raw []byte
		err = rows.Scan(&position, &raw)
		if err != nil {
			break
		}
		step := new(workflow.StepResult)
		err = json.Unmarshal(raw, step)
		if err != nil {
			break
		}
		steps[position] = step
		positions = append(positions, position)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return r, err
	}

	rows, err = db.Query("SELECT position, record FROM attempts WHERE run_id = ? ORDER BY number", id)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var position string
		var raw []byte
		err = rows.Scan(&position, &raw)
		if err != nil {
			break
		}
		var attempt workflow.Attempt
		err = json.Unmarshal(raw, &attempt)
		if err != nil {
			break
		}
		step := steps[position]
		if step == nil {
			err = fmt.Errorf("attempt has no invocation %q", position)
			break
		}
		step.Attempts = append(step.Attempts, attempt)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return r, err
	}

	// Children are assembled before parents, preserving input order regardless
	// of the order in which workers checkpointed their outcomes.
	for _, position := range positions {
		parent, ordinal := "", position
		separator := strings.LastIndexByte(position, '/')
		if separator >= 0 {
			parent, ordinal = position[:separator], position[separator+1:]
		}
		index, parseErr := strconv.Atoi(ordinal)
		if parseErr != nil || index < 0 || index >= workflow.MaxIterations {
			return r, fmt.Errorf("invalid invocation position %q", position)
		}
		target := &r.Steps
		if parent != "" {
			owner := steps[parent]
			if owner == nil {
				return r, fmt.Errorf("missing invocation parent %q", parent)
			}
			target = &owner.Iterations
		}
		for len(*target) <= index {
			*target = append(*target, workflow.StepResult{})
		}
		(*target)[index] = *steps[position]
	}
	return r, nil
}

func (s *Store) readRun(id string) (workflow.Run, error) {
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return workflow.Run{}, err
	}
	defer tx.Rollback()
	r, err := loadRun(tx, id)
	if err != nil {
		return r, err
	}
	return r, tx.Commit()
}
