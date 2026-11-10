package records

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/automation/store"
	"github.com/keshon/gektor/internal/workspace"
)

func TestReadMissingRecordAndCreateWithReturnedRevision(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	directory := t.TempDir()
	s, err := store.OpenStore(directory, ws.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	read := Operation{WS: ws, StoreDir: directory}
	args := json.RawMessage(`{"kind":"notes","id":"state","allow_missing":true}`)
	if _, err := read.Run(t.Context(), args); err == nil || errors.Is(err, store.ErrEntryNotFound) {
		t.Fatal("missing schema treated as missing entry", err)
	}
	if _, err := s.RegisterSchema(t.Context(), "notes", []byte(`{"type":"object","properties":{"value":{"type":"integer"}},"required":["value"],"additionalProperties":false}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := read.Run(t.Context(), json.RawMessage(`{"kind":"notes","id":"state"}`)); !errors.Is(err, store.ErrEntryNotFound) {
		t.Fatal("strict read did not fail", err)
	}
	out, err := read.Run(t.Context(), args)
	var missing struct {
		Found    bool            `json:"found"`
		Revision uint64          `json:"revision"`
		Data     json.RawMessage `json:"data"`
	}
	if err != nil || json.Unmarshal([]byte(out), &missing) != nil || missing.Found || missing.Revision != 0 || string(missing.Data) != "null" {
		t.Fatal(out, err)
	}
	if _, err := s.Entry("notes", "state"); !errors.Is(err, store.ErrEntryNotFound) {
		t.Fatal("read created a record", err)
	}
	write := Operation{WS: ws, StoreDir: directory, Write: true}
	create, _ := json.Marshal(map[string]any{"kind": "notes", "id": "state", "expected_revision": missing.Revision, "data": map[string]int{"value": 7}})
	if _, err := write.Run(t.Context(), create); err != nil {
		t.Fatal(err)
	}
	if _, err := write.Run(t.Context(), create); !errors.Is(err, store.ErrConflict) {
		t.Fatal("stale create overwrote an existing record", err)
	}
	out, err = read.Run(t.Context(), args)
	var found struct {
		Found    bool   `json:"found"`
		Revision uint64 `json:"revision"`
	}
	if err != nil || json.Unmarshal([]byte(out), &found) != nil || !found.Found || found.Revision != 1 {
		t.Fatal(out, err)
	}
	out, err = read.Run(t.Context(), json.RawMessage(`{"kind":"notes","id":"state"}`))
	var strict map[string]any
	if err != nil || json.Unmarshal([]byte(out), &strict) != nil {
		t.Fatal(out, err)
	}
	if _, ok := strict["found"]; ok {
		t.Fatal("strict output shape changed")
	}
	paths, err := filepath.Glob(filepath.Join(directory, "*", "records", "notes", "state.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	if err := os.WriteFile(paths[0], []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := read.Run(t.Context(), args); err == nil || errors.Is(err, store.ErrEntryNotFound) {
		t.Fatal("corrupt record treated as missing", err)
	}
}
