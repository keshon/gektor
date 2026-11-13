package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

func TestSQLiteConfigurationAndConcurrentReader(t *testing.T) {
	s, base, work := testStore(t)
	var journal string
	err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journal)
	if err != nil || journal != "wal" {
		t.Fatal(journal, err)
	}
	var sync, foreignKeys int
	err = s.db.QueryRow("PRAGMA synchronous").Scan(&sync)
	if err != nil || sync != 2 {
		t.Fatal("full synchronization disabled", sync, err)
	}
	err = s.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys)
	if err != nil || foreignKeys != 1 {
		t.Fatal("foreign keys disabled", foreignKeys, err)
	}
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r := collectionRun(2)
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := OpenStore(base, work)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	before, err := loadRun(tx, w.ID())
	if err != nil {
		t.Fatal(err)
	}
	r.Steps[0].Iterations[1].Status = "completed"
	err = w.Save(r, 0, 1)
	if err != nil {
		t.Fatal("reader blocked a checkpoint", err)
	}
	after, err := loadRun(tx, w.ID())
	if err != nil || before.Steps[0].Iterations[1].Status != after.Steps[0].Iterations[1].Status {
		t.Fatal("read transaction lost its snapshot", err)
	}
	err = tx.Commit()
	if err != nil {
		t.Fatal(err)
	}
	current, err := reader.Run(w.ID())
	if err != nil || current.Record.Steps[0].Iterations[1].Status != "completed" {
		t.Fatal("new reader missed checkpoint", err)
	}
}

func collectionRun(count int) workflow.Run {
	children := make([]workflow.StepResult, count)
	for index := range children {
		children[index] = workflow.StepResult{
			ID: fmt.Sprintf("map[%d]", index), Status: "pending", Args: json.RawMessage(`{}`),
		}
	}
	return workflow.Run{
		Status: "running", Started: time.Now().UTC(),
		Definition: workflow.Definition{Version: 4, Name: "checkpoint test"},
		Steps:      []workflow.StepResult{{ID: "map", Status: "running", Iterations: children}},
	}
}

func TestItemCheckpointWritesOnlyChangedInvocation(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r := collectionRun(1000)
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`
CREATE TABLE checkpoint_writes (position TEXT);
CREATE TRIGGER audit_checkpoint AFTER INSERT ON invocations BEGIN
  INSERT INTO checkpoint_writes VALUES (NEW.position);
END;
CREATE TRIGGER forbid_snapshot_rewrite BEFORE UPDATE OF record ON runs BEGIN
  SELECT RAISE(ABORT, 'rewrote frozen run snapshot');
END;`)
	if err != nil {
		t.Fatal(err)
	}

	child := &r.Steps[0].Iterations[7]
	child.Status, child.Output = "completed", `{"value":7}`
	finished := time.Now().UTC()
	child.Attempts = []workflow.Attempt{{Number: 1, Started: r.Started, Finished: &finished}}
	err = w.Save(r, 0, 7)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	var position string
	err = s.db.QueryRow("SELECT count(*), min(position) FROM checkpoint_writes").Scan(&count, &position)
	if err != nil || count != 1 || position != "0/7" {
		t.Fatal("rewrote unrelated invocations", count, position, err)
	}
	state, err := s.Run(w.ID())
	if err != nil {
		t.Fatal(err)
	}
	items := state.Record.Steps[0].Iterations
	if len(items) != 1000 || items[7].Output != child.Output || len(items[7].Attempts) != 1 || items[8].Status != "pending" {
		t.Fatal("checkpoint did not round trip", len(items))
	}
}

func TestCheckpointFailureRollsBackRunAndInvocationTogether(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r := collectionRun(2)
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`CREATE TRIGGER reject_item BEFORE INSERT ON invocations
WHEN NEW.position = '0/1' BEGIN SELECT RAISE(ABORT, 'storage fault'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	r.Status = "completed"
	r.Steps[0].Iterations[1].Status = "completed"
	err = w.Save(r, 0, 1)
	if err == nil {
		t.Fatal("checkpoint fault was ignored")
	}
	state, err := s.Run(w.ID())
	if err != nil {
		t.Fatal(err)
	}
	if state.Record.Status != "running" || state.Record.Steps[0].Iterations[1].Status != "pending" {
		t.Fatal("partial transaction became visible", state)
	}
}

func TestNestedCheckpointPreservesScopeSnapshotsAndRollsBack(t *testing.T) {
	s, _, _ := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	leaf := collectionRun(1000)
	middle := workflow.Run{Status: "running", Definition: workflow.Definition{Version: 4, Name: "middle"}, Steps: []workflow.StepResult{{ID: "inner", Status: "running", Child: &leaf}}}
	args := json.RawMessage(`{"exact":9007199254740993,"decimal":1.2345678901234567890123456789}`)
	r := workflow.Run{Status: "running", Steps: []workflow.StepResult{{ID: "outer", Status: "running", Args: args, Child: &middle}}}
	err = w.Save(r)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`
CREATE TABLE checkpoint_writes (position TEXT);
CREATE TRIGGER audit_checkpoint AFTER INSERT ON invocations BEGIN
 INSERT INTO checkpoint_writes VALUES (NEW.position);
END;
CREATE TRIGGER forbid_run_snapshot BEFORE UPDATE OF record ON runs BEGIN
 SELECT RAISE(ABORT, 'rewrote run snapshot');
END;
CREATE TRIGGER forbid_scope_snapshot BEFORE UPDATE OF record ON scopes BEGIN
 SELECT RAISE(ABORT, 'rewrote scope snapshot');
END;`)
	if err != nil {
		t.Fatal(err)
	}
	r.Status, r.Steps[0].Status = "waiting_review", "waiting_review"
	middle.Status, middle.Steps[0].Status = "waiting_review", "waiting_review"
	leaf.Status = "waiting_review"
	leaf.Steps[0].Iterations[7].Status = "completed"
	leaf.Steps[0].Iterations[7].Output = `7`
	path := []int{0, workflow.ChildScope, 0, workflow.ChildScope, 0, 7}
	err = w.Save(r, path...)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	var position string
	err = s.db.QueryRow("SELECT count(*), min(position) FROM checkpoint_writes").Scan(&count, &position)
	if err != nil || count != 1 || position != "0/call/0/call/0/7" {
		t.Fatal("rewrote unrelated invocations", count, position, err)
	}
	state, err := s.Run(w.ID())
	if err != nil {
		t.Fatal(err)
	}
	outer := state.Record.Steps[0]
	if string(outer.Args) != string(args) {
		t.Fatal("ancestor status update changed numeric arguments", string(outer.Args))
	}
	inner := outer.Child.Steps[0]
	loaded := inner.Child
	if state.Record.Status != "waiting_review" || outer.Status != "waiting_review" || outer.Child.Status != "waiting_review" || inner.Status != "waiting_review" || loaded.Status != "waiting_review" || loaded.Definition.Name != leaf.Definition.Name || loaded.Steps[0].Iterations[7].Output != "7" || loaded.Steps[0].Iterations[8].Status != "pending" {
		t.Fatal("nested checkpoint lost scope state", state)
	}
	_, err = s.db.Exec(`CREATE TRIGGER reject_nested BEFORE INSERT ON invocations
WHEN NEW.position = '0/call/0/call/0/7' BEGIN SELECT RAISE(ABORT, 'nested storage fault'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	r.Status, middle.Status, leaf.Status = "completed", "completed", "completed"
	r.Steps[0].Status, middle.Steps[0].Status = "completed", "completed"
	leaf.Steps[0].Iterations[7].Output = `999`
	err = w.Save(r, path...)
	if err == nil {
		t.Fatal("nested fault was ignored")
	}
	state, err = s.Run(w.ID())
	if err != nil {
		t.Fatal(err)
	}
	outer = state.Record.Steps[0]
	inner = outer.Child.Steps[0]
	if state.Record.Status != "waiting_review" || outer.Status != "waiting_review" || outer.Child.Status != "waiting_review" || inner.Status != "waiting_review" || inner.Child.Status != "waiting_review" || inner.Child.Steps[0].Iterations[7].Output != "7" {
		t.Fatal("nested transaction partially committed", state)
	}
}

func BenchmarkInvocationCheckpoint(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			s, err := OpenStore(b.TempDir(), b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				w, err := s.NewRun()
				if err != nil {
					b.Fatal(err)
				}
				r := collectionRun(count)
				err = w.Save(r)
				if err != nil {
					b.Fatal(err)
				}
				for index := range count {
					child := &r.Steps[0].Iterations[index]
					child.Status, child.Output = "completed", `{"value":1}`
					err = w.Save(r, 0, index)
					if err != nil {
						b.Fatal(err)
					}
				}
				r.Status = "completed"
				err = w.Save(r)
				if err != nil {
					b.Fatal(err)
				}
				err = w.Close()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
