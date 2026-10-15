package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/keshon/tars/internal/llm"
)

func TestReadHistorySharesSnapshotAndLogRecoveryPolicy(t *testing.T) {
	for _, snapshot := range []string{"valid", "corrupt", "missing"} {
		t.Run(snapshot, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			saved := []llm.Message{{Role: llm.RoleUser, Content: "saved snapshot"}}
			logged := []llm.Message{{Role: llm.RoleUser, Content: "recoverable log"}}
			if err := Append(path, 1, logged); err != nil {
				t.Fatal(err)
			}
			data := []byte("broken snapshot")
			if snapshot == "valid" {
				var err error
				data, err = json.Marshal(saved)
				if err != nil {
					t.Fatal(err)
				}
			}
			if snapshot != "missing" {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			want := logged
			if snapshot == "valid" {
				want = saved
			}
			for _, input := range []string{path, dir} {
				got, err := ReadHistory(input)
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: history=%v error=%v", input, got, err)
				}
			}
			got, err := ReadHistory(LogPath(path))
			if err != nil || !reflect.DeepEqual(got, logged) {
				t.Fatalf("explicit log: history=%v error=%v", got, err)
			}
			_, err = ReadSnapshot(path)
			if (err == nil) != (snapshot == "valid") {
				t.Fatal("snapshot-only read unexpectedly recovered from the log")
			}
		})
	}
}

func TestReadHistoryFailsWithoutAValidCheckpoint(t *testing.T) {
	if _, err := ReadHistory(t.TempDir()); err == nil {
		t.Fatal("missing snapshot and log accepted")
	}
}
