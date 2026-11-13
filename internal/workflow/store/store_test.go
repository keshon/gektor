package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/keshon/gektor/internal/workflow"
)

func testStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	base, work := t.TempDir(), t.TempDir()
	s, err := OpenStore(base, work)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, base, work
}

func TestRunLeaseAndWorkspaceIsolation(t *testing.T) {
	s, base, work := testStore(t)
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Save(workflow.Run{Status: "running", Definition: workflow.Definition{Name: "Example"}}); err != nil {
		t.Fatal(err)
	}
	other, err := OpenStore(base, work)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	state, err := other.Run(w.ID())
	if err != nil || state.Status != "running" {
		t.Fatal(state, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	state, err = other.Run(w.ID())
	if err != nil || state.Status != "interrupted" || state.Record.Status != "running" {
		t.Fatal(state, err)
	}
	if err := w.Save(workflow.Run{Status: "completed"}); err == nil {
		t.Fatal("closed writer accepted a save")
	}
	isolated, err := OpenStore(base, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer isolated.Close()
	if _, err := isolated.Run(w.ID()); err == nil {
		t.Fatal("run leaked across workspaces")
	}
	for _, id := range []string{"../escape", "CON", "", "a/b"} {
		if _, err := s.Run(id); err == nil {
			t.Fatal("accepted invalid ID", id)
		}
	}
	if _, err := OpenStore(filepath.Join(work, "state"), work); err == nil {
		t.Fatal("accepted store inside workspace")
	}
}

func TestRunLeaseProcessExit(t *testing.T) {
	if base := os.Getenv("GEKTOR_TEST_LEASE_BASE"); base != "" {
		s, err := OpenStore(base, os.Getenv("GEKTOR_TEST_LEASE_WORK"))
		if err != nil {
			t.Fatal(err)
		}
		w, err := s.NewRun()
		if err != nil {
			t.Fatal(err)
		}
		leaf := collectionRun(2)
		r := leaf
		path := []int{0, 1}
		if os.Getenv("GEKTOR_TEST_LEASE_NESTED") == "1" {
			r = workflow.Run{Status: "running", Steps: []workflow.StepResult{{ID: "call", Status: "running", Child: &leaf}}}
			path = []int{0, workflow.ChildScope, 0, 1}
		}
		err = w.Save(r)
		if err != nil {
			t.Fatal(err)
		}
		leaf.Steps[0].Iterations[1].Status = "completed"
		leaf.Steps[0].Iterations[1].Output = `{"acknowledged":true}`
		err = w.Save(r, path...)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(filepath.Join(base, "id"), []byte(w.ID()), 0600)
		if err != nil {
			t.Fatal(err)
		}
		// Simulate abrupt exit: no deferred Close calls run.
		os.Exit(0)
	}
	for _, nested := range []string{"0", "1"} {
		t.Run("nested"+nested, func(t *testing.T) {
			s, base, work := testStore(t)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			c := exec.Command(exe, "-test.run=^TestRunLeaseProcessExit$")
			c.Env = append(os.Environ(), "GEKTOR_TEST_LEASE_BASE="+base, "GEKTOR_TEST_LEASE_WORK="+work, "GEKTOR_TEST_LEASE_NESTED="+nested)
			output, err := c.CombinedOutput()
			if err != nil {
				t.Fatal(err, string(output))
			}
			id, err := os.ReadFile(filepath.Join(base, "id"))
			if err != nil {
				t.Fatal(err)
			}
			r, err := s.Run(string(id))
			if err != nil || r.Status != "interrupted" {
				t.Fatal(r, err)
			}
			items := r.Record.Steps[0].Iterations
			if nested == "1" {
				items = r.Record.Steps[0].Child.Steps[0].Iterations
			}
			if items[1].Output != `{"acknowledged":true}` || items[0].Status != "pending" {
				t.Fatal("acknowledged invocation lost after process exit", r)
			}
		})
	}
}

func TestCorruptRunsRemainVisible(t *testing.T) {
	s, _, _ := testStore(t)
	healthy, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer healthy.Close()
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	err = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec("UPDATE runs SET record = ? WHERE id = ?", []byte("broken JSON"), w.ID())
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs()
	if err != nil || len(runs) != 2 {
		t.Fatal(runs, err)
	}
	found := false
	for _, r := range runs {
		if r.ID == w.ID() {
			found = r.Status == "unreadable" && r.Error != ""
		}
	}
	if !found {
		t.Fatal("corruption was hidden", runs)
	}
}

const entrySchema = `{"type":"object","required":["value"],"properties":{"value":{"type":"integer"}},"additionalProperties":false}`

func TestEntriesValidateAndRejectStaleUpdates(t *testing.T) {
	s, base, work := testStore(t)
	digest, err := s.RegisterSchema(t.Context(), "notes", []byte(entrySchema))
	if err != nil {
		t.Fatal(err)
	}
	if again, err := s.RegisterSchema(t.Context(), "notes", []byte(entrySchema)); err != nil || again != digest {
		t.Fatal(again, err)
	}
	if _, err := s.RegisterSchema(t.Context(), "notes", []byte(`{"type":"object"}`)); err == nil {
		t.Fatal("replaced registered schema")
	}
	e, err := s.PutEntry(t.Context(), "notes", "one", 0, json.RawMessage(`{"value":9007199254740993}`))
	if err != nil || e.Revision != 1 || e.SchemaSHA256 != digest {
		t.Fatal(e, err)
	}
	if !strings.Contains(string(e.Data), "9007199254740993") {
		t.Fatal("lost integer precision")
	}
	for _, data := range []string{`{"value":"wrong"}`, `{}`, `{"value":2,"unknown":true}`, `null`, `{} {}`, strings.Repeat(" ", 64<<10) + "{}"} {
		if _, err := s.PutEntry(t.Context(), "notes", "one", 1, json.RawMessage(data)); err == nil {
			t.Fatal("accepted invalid data", data)
		}
	}
	other, err := OpenStore(base, work)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, store := range []*Store{s, other} {
		wg.Go(func() {
			_, err := store.PutEntry(t.Context(), "notes", "one", 1, json.RawMessage(`{"value":2}`))
			results <- err
		})
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflict)
	}
	e, err = other.Entry("notes", "one")
	if err != nil || e.Revision != 2 {
		t.Fatal(e, err)
	}
	lease, err := s.lockKind(t.Context(), "notes")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := other.PutEntry(ctx, "notes", "one", 2, json.RawMessage(`{"value":3}`)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSchemaReferencesAndRecordNames(t *testing.T) {
	s, _, _ := testStore(t)
	for _, schema := range []string{
		`{"type":"object","$ref":"https://example.invalid/schema"}`,
		`{"type":"object","$ref":"file:///private.json"}`,
		`{"type":"object","properties":{"v":{"type":"nonexistent"}}}`,
	} {
		if _, err := s.RegisterSchema(t.Context(), "notes", []byte(schema)); err == nil {
			t.Fatal("accepted external or invalid schema", schema)
		}
	}
	if _, err := s.RegisterSchema(t.Context(), "notes", []byte(`{"type":"object","$defs":{"n":{"type":"integer"}},"properties":{"v":{"$ref":"#/$defs/n"}}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutEntry(t.Context(), "notes", "one", 0, json.RawMessage(`{"v":3}`)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"schema", "../outside", "Upper", "con", "lpt1", ""} {
		if _, err := s.PutEntry(t.Context(), "notes", id, 0, json.RawMessage(`{}`)); err == nil {
			t.Fatal("accepted unsafe ID", id)
		}
	}
}
