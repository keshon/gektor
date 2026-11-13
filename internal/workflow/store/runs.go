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

// Save checkpoints the whole run when path is empty. Indexes select steps or
// iterations; ChildScope enters a call's child steps. The engine owns this path.
// The writer's process lease excludes other writers for this run.
func (w *RunWriter) Save(r workflow.Run, path ...int) error {
	_, err := w.lease.Stat()
	if err != nil {
		return fmt.Errorf("run writer closed: %w", err)
	}
	steps := r.Steps
	parent, position := "", ""
	var selected workflow.StepResult
	statuses := map[string]string{}
	var scopes []struct {
		position string
		run      *workflow.Run
	}
	for offset, index := range path {
		if index == workflow.ChildScope && offset > 0 && offset < len(path)-1 && path[offset-1] != workflow.ChildScope {
			if selected.Child == nil {
				return fmt.Errorf("checkpoint child scope missing")
			}
			parent = position + "/call"
			steps = selected.Child.Steps
			scopes = append(scopes, struct {
				position string
				run      *workflow.Run
			}{parent, selected.Child})
			continue
		}
		if index < 0 || index >= len(steps) {
			return fmt.Errorf("checkpoint index out of range")
		}
		selected = steps[index]
		position = invocationPosition(parent, index)
		if offset < len(path)-1 {
			statuses[position] = selected.Status
		}
		parent = position
		steps = selected.Iterations
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
		rSteps := r.Steps
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
		_, err = tx.Exec("DELETE FROM scopes WHERE run_id = ?", w.id)
		if err != nil {
			return err
		}
		for index, step := range rSteps {
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
	for _, scope := range scopes {
		var finish []byte
		finish, err = json.Marshal(scope.run.Finished)
		if err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE scopes SET status = ?, finished = ? WHERE run_id = ? AND position = ?", scope.run.Status, finish, w.id, scope.position)
		if err != nil {
			return err
		}
	}
	for position, status := range statuses {
		_, err = tx.Exec(`UPDATE invocations SET record = json_set(record, '$.status', ?)
WHERE run_id = ? AND position = ? AND json_extract(record, '$.status') != ?`, status, w.id, position, status)
		if err != nil {
			return err
		}
	}
	parent = ""
	separator := strings.LastIndexByte(position, '/')
	if separator >= 0 {
		parent = position[:separator]
	}
	_, err = tx.Exec("DELETE FROM invocations WHERE run_id = ? AND position = ?", w.id, position)
	if err != nil {
		return err
	}
	// The position prefix covers only this invocation's descendants and uses the
	// primary key. Scope headers and invocation changes commit together.
	_, err = tx.Exec("DELETE FROM invocations WHERE run_id = ? AND position >= ? AND position < ?", w.id, position+"/", position+"0")
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM scopes WHERE run_id = ? AND position >= ? AND position < ?", w.id, position+"/", position+"0")
	if err != nil {
		return err
	}
	err = saveInvocation(tx, w.id, parent, path[len(path)-1], selected)
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
	children, attempts, childScope := step.Iterations, step.Attempts, step.Child
	step.Iterations, step.Attempts, step.Child = nil, nil, nil
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
	if childScope != nil {
		header := *childScope
		header.Steps = nil
		record, err = json.Marshal(header)
		if err != nil {
			return err
		}
		var finished []byte
		finished, err = json.Marshal(childScope.Finished)
		if err != nil {
			return err
		}
		scopePosition := position + "/call"
		_, err = tx.Exec("INSERT INTO scopes VALUES (?, ?, ?, ?, ?)", id, scopePosition, childScope.Status, finished, record)
		if err != nil {
			return err
		}
		for index, step := range childScope.Steps {
			err = saveInvocation(tx, id, scopePosition, index, step)
			if err != nil {
				return err
			}
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

	rows, err = db.Query("SELECT position, status, finished, record FROM scopes WHERE run_id = ?", id)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var position, status string
		var record, finished []byte
		err = rows.Scan(&position, &status, &finished, &record)
		if err != nil {
			break
		}
		child := new(workflow.Run)
		err = json.Unmarshal(record, child)
		if err != nil {
			break
		}
		child.Status = status
		err = json.Unmarshal(finished, &child.Finished)
		if err != nil {
			break
		}
		owner := steps[strings.TrimSuffix(position, "/call")]
		if owner == nil || !strings.HasSuffix(position, "/call") {
			err = fmt.Errorf("missing child scope owner %q", position)
			break
		}
		owner.Child = child
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
			ownerPosition := strings.TrimSuffix(parent, "/call")
			owner := steps[ownerPosition]
			if owner == nil {
				return r, fmt.Errorf("missing invocation parent %q", parent)
			}
			target = &owner.Iterations
			if ownerPosition != parent {
				if owner.Child == nil {
					return r, fmt.Errorf("missing child scope %q", parent)
				}
				target = &owner.Child.Steps
			}
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
