package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/llm"
)

func TestSnapshotRetainsArchiveAndRoundTripsContext(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleSystem, Content: "sys"}, {Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleAssistant, Content: "old answer"}, {Role: llm.RoleUser, Content: "old question"}, {Role: llm.RoleAssistant, Content: "latest answer"}}
	path := filepath.Join(t.TempDir(), "state.json")
	// A legacy array is read without a destructive migration.
	data, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	active := append(append([]llm.Message(nil), history[:2]...), llm.Message{Role: llm.RoleUser, Content: "summary"}, history[4])
	s.Compact(active)
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	archive, err := ReadHistory(path)
	if err != nil || !reflect.DeepEqual(archive, history) {
		t.Fatalf("archive lost: %+v %v", archive, err)
	}
	context, err := ReadContext(path)
	if err != nil || !reflect.DeepEqual(context, active) {
		t.Fatalf("projection lost: %+v %v", context, err)
	}
}

func TestRunRangesSurviveRepeatedCompaction(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleSystem}, {Role: llm.RoleUser, Content: "task"}, {Role: llm.RoleAssistant, Content: "first"}, {Role: llm.RoleUser, Content: "next"}, {Role: llm.RoleAssistant, Content: "last"}}
	s := New(history)
	s.Runs = []Run{{Start: 1, End: 3, Outcome: "completed", Steps: 1}, {Start: 3, End: 5, Outcome: "completed", Steps: 1}}
	for _, start := range []int{3, 4} {
		projection := append(append([]llm.Message(nil), history[:2]...), llm.Message{Role: llm.RoleUser, Content: "summary"})
		s.Compact(append(projection, history[start:]...))
	}
	data, _ := json.Marshal(s)
	loaded, err := Decode(data)
	if err != nil || !reflect.DeepEqual(loaded.Runs, s.Runs) || !reflect.DeepEqual(loaded.History, history) {
		t.Fatalf("lost archive or run ranges: %+v %v", loaded, err)
	}
	for _, r := range []Run{{Start: -1, End: 2}, {Start: 1, End: 6}, {Start: 4, End: 2}} {
		s.Runs = []Run{r}
		data, _ := json.Marshal(s)
		if _, err := Decode(data); err == nil {
			t.Fatalf("accepted invalid range %+v", r)
		}
	}
}

func TestSnapshotRejectsInvalidContextAndCleansFailedWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	for _, body := range []string{`{"version":2}`, `{"version":1,"context_start":-1}`, `{"version":1,"history":[],"context_start":3,"summary":"s"}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("accepted malformed context: %s", body)
		}
	}
	target := filepath.Join(dir, "directory")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := New(nil).Save(target); err == nil {
		t.Fatal("directory accepted as snapshot")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".state-") {
			t.Fatal("temporary snapshot leaked")
		}
	}
}
