package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactProcessExitRecovery(t *testing.T) {
	if base := os.Getenv("GEKTOR_ARTIFACT_CRASH_BASE"); base != "" {
		artifactCrashChild(t, base)
		return
	}
	for _, phase := range []string{"reserved", "stream", "synced", "published", "deleting", "deleted"} {
		t.Run(phase, func(t *testing.T) {
			s, base, work := testStore(t)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, exe, "-test.run=^TestArtifactProcessExitRecovery$")
			child.Env = append(os.Environ(),
				"GEKTOR_ARTIFACT_CRASH_BASE="+base,
				"GEKTOR_ARTIFACT_CRASH_WORK="+work,
				"GEKTOR_ARTIFACT_CRASH_PHASE="+phase,
			)
			output, err := child.CombinedOutput()
			if err != nil {
				t.Fatal(err, string(output))
			}
			raw, err := os.ReadFile(filepath.Join(base, "crashed-run"))
			if err != nil {
				t.Fatal(err)
			}
			id := string(raw)
			state, err := s.Run(id)
			if err != nil || state.Status != "interrupted" {
				t.Fatal("process lease was not released", state, err)
			}
			err = s.ReconcileArtifacts(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			writer, _, err := s.EditRun(id)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			items, err := writer.ListArtifacts()
			if err != nil {
				t.Fatal(err)
			}
			if phase == "published" {
				if len(items) != 1 {
					t.Fatal("committed artifact lost", items)
				}
				_, payload, readErr := writer.Artifacts("read").Read(t.Context(), items[0].ID)
				if readErr != nil || string(payload) != "payload" {
					t.Fatal("committed bytes lost", string(payload), readErr)
				}
				return
			}
			var count int
			err = s.db.QueryRow("SELECT count(*) FROM artifacts WHERE run_id = ?", id).Scan(&count)
			if err != nil || count != 0 || len(items) != 0 {
				t.Fatal("abandoned reservation retained", count, items, err)
			}
			dir, err := s.root.Open(writer.artifactDirectory())
			if os.IsNotExist(err) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			entries, err := dir.ReadDir(-1)
			if err != nil || len(entries) != 0 {
				t.Fatal("abandoned payload retained", entries, err)
			}
		})
	}
}

func artifactCrashChild(t *testing.T, base string) {
	t.Helper()
	s, err := OpenStore(base, os.Getenv("GEKTOR_ARTIFACT_CRASH_WORK"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(base, "crashed-run"), []byte(w.id), 0600)
	if err != nil {
		t.Fatal(err)
	}
	phase := os.Getenv("GEKTOR_ARTIFACT_CRASH_PHASE")
	if phase == "stream" {
		_, err = w.Artifacts("crash").Put(t.Context(), "partial.bin", &artifactCrashReader{})
		t.Fatal("stream did not exit", err)
	}
	if phase == "published" || phase == "deleting" || phase == "deleted" {
		_, err = w.Artifacts("crash").Put(t.Context(), "payload.txt", strings.NewReader("payload"))
		if err != nil {
			t.Fatal(err)
		}
		if phase != "published" {
			_, err = s.db.Exec("UPDATE artifacts SET status = 'deleting' WHERE run_id = ?", w.id)
			if err != nil {
				t.Fatal(err)
			}
		}
		if phase == "deleted" {
			err = s.root.RemoveAll(w.artifactDirectory())
			if err != nil {
				t.Fatal(err)
			}
		}
		os.Exit(0)
	}
	_, err = w.reserveArtifact(t.Context(), "unfinished")
	if err != nil {
		t.Fatal(err)
	}
	if phase == "synced" {
		dir := filepath.Join(w.artifactDirectory(), "unfinished")
		err = s.root.MkdirAll(dir, 0700)
		if err != nil {
			t.Fatal(err)
		}
		f, openErr := s.root.Create(filepath.Join(dir, "data"))
		if openErr != nil {
			t.Fatal(openErr)
		}
		_, err = f.WriteString("payload")
		if err != nil {
			t.Fatal(err)
		}
		err = f.Sync()
		if err != nil {
			t.Fatal(err)
		}
		err = f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	// Exit without deferred writer/store cleanup at the selected durable boundary.
	os.Exit(0)
}

type artifactCrashReader struct {
	read bool
}

func (r *artifactCrashReader) Read(p []byte) (int, error) {
	if r.read {
		os.Exit(0)
	}
	r.read = true
	clear(p)
	return len(p), nil
}
