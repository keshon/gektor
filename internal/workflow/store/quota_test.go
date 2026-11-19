package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workflow"
)

// Retained metadata reaches quota boundaries without writing gigabytes of files.
func retainedArtifacts(t *testing.T, s *Store, count int, bytes int64) {
	t.Helper()
	var ids []string
	old := time.Now().Add(-48 * time.Hour)
	perRun := MaxRunArtifacts
	if bytes > 0 {
		perRun = min(perRun, int(MaxRunArtifactBytes/bytes))
	}
	for offset := 0; offset < count; offset += perRun {
		writer, err := s.NewRun()
		if err != nil {
			t.Fatal(err)
		}
		err = writer.Save(workflow.Run{Status: "completed", Finished: &old})
		closeErr := writer.Close()
		if err != nil || closeErr != nil {
			t.Fatal(err, closeErr)
		}
		ids = append(ids, writer.ID())
	}
	tx, err := s.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := range count {
		runID := ids[i/perRun]
		id := fmt.Sprintf("existing-%d", i)
		meta := workflow.Artifact{ID: id, RunID: runID, Name: "fixture", Size: bytes}
		raw, encodeErr := json.Marshal(meta)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		_, err = tx.Exec("INSERT INTO artifacts VALUES (?, ?, 'published', ?, ?)", runID, id, bytes, raw)
		if err != nil {
			t.Fatal(err)
		}
	}
	err = tx.Commit()
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceArtifactQuotaExactFitAndRetentionReleasesCapacity(t *testing.T) {
	for _, countLimit := range []bool{false, true} {
		t.Run(fmt.Sprint(countLimit), func(t *testing.T) {
			s, base, work := testStore(t)
			if countLimit {
				retainedArtifacts(t, s, MaxWorkspaceArtifacts-1, 0)
			} else {
				retainedArtifacts(t, s, MaxWorkspaceArtifactBytes/MaxArtifactBytes, MaxArtifactBytes)
			}
			_, err := s.db.Exec("UPDATE artifacts SET status='deleting' WHERE rowid=(SELECT min(rowid) FROM artifacts)")
			if err != nil {
				t.Fatal(err)
			}
			other, err := OpenStore(base, work)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			writer, err := other.NewRun()
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			_, err = writer.Artifacts("boundary").Put(t.Context(), "empty", strings.NewReader(""))
			if err != nil {
				t.Fatal("exact workspace boundary rejected", err)
			}
			_, err = writer.Artifacts("overflow").Put(t.Context(), "positive", strings.NewReader("x"))
			if err == nil || !strings.Contains(err.Error(), "workspace") {
				t.Fatal("workspace quota exceeded", err)
			}
			status, err := s.Status(t.Context())
			if err != nil || status.Artifacts["writing"].Count != 0 {
				t.Fatal("failed write leaked reservation", status, err)
			}
			chargedCount := status.Artifacts["published"].Count + status.Artifacts["deleting"].Count
			chargedBytes := status.Artifacts["published"].Bytes + status.Artifacts["deleting"].Bytes
			if countLimit && chargedCount != MaxWorkspaceArtifacts {
				t.Fatal("count quota drifted", status)
			}
			if !countLimit && chargedBytes != MaxWorkspaceArtifactBytes {
				t.Fatal("byte quota drifted", status)
			}
			removed, err := s.PruneRuns(t.Context(), time.Now().Add(-24*time.Hour))
			if err != nil || len(removed) == 0 {
				t.Fatal("retention did not release workspace quota", removed, err)
			}
			_, err = writer.Artifacts("reused").Put(t.Context(), "next", strings.NewReader("next"))
			if err != nil {
				t.Fatal("another store handle did not see released quota", err)
			}
		})
	}
}

func TestWorkspaceArtifactQuotaAcrossProcesses(t *testing.T) {
	if base := os.Getenv("GEKTOR_QUOTA_BASE"); base != "" {
		quotaChild(t, base)
		return
	}
	s, base, work := testStore(t)
	retainedArtifacts(t, s, MaxWorkspaceArtifactBytes/MaxArtifactBytes-1, MaxArtifactBytes)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var children []*exec.Cmd
	t.Cleanup(func() {
		cancel()
		for _, child := range children {
			if child.ProcessState == nil {
				// Cleanup also covers children intentionally killed after a test failure.
				child.Process.Kill()
				child.Wait()
			}
		}
	})
	for _, name := range []string{"a", "b"} {
		child := exec.CommandContext(ctx, executable, "-test.run=^TestWorkspaceArtifactQuotaAcrossProcesses$")
		child.Env = append(os.Environ(), "GEKTOR_QUOTA_BASE="+base, "GEKTOR_QUOTA_WORK="+work, "GEKTOR_QUOTA_NAME="+name)
		err = child.Start()
		if err != nil {
			t.Fatal(err)
		}
		children = append(children, child)
	}
	deadline := time.Now().Add(10 * time.Second)
	var winner string
	for winner == "" && time.Now().Before(deadline) {
		for _, name := range []string{"a", "b"} {
			_, err = os.Stat(filepath.Join(base, "acquired-"+name))
			if err == nil {
				winner = name
				break
			}
			if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if winner == "" {
		t.Fatal("neither process acquired remaining capacity")
	}
	loser := "a"
	if winner == "a" {
		loser = "b"
	}
	// Both children start before checking the losing process's quota wait.
	for {
		_, err = os.Stat(filepath.Join(base, "ready-"+loser))
		if err == nil {
			break
		}
		if !os.IsNotExist(err) || time.Now().After(deadline) {
			t.Fatal("second process did not start", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(2 * artifactQuotaPoll)
	_, err = os.Stat(filepath.Join(base, "acquired-"+loser))
	if !os.IsNotExist(err) {
		t.Fatal("processes overbooked workspace capacity", err)
	}
	writer, err := s.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	waitCtx, stopWait := context.WithTimeout(t.Context(), 100*time.Millisecond)
	_, err = writer.Artifacts("cancelled").Put(waitCtx, "file", strings.NewReader("data"))
	stopWait()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("workspace quota wait ignored cancellation", err)
	}
	err = os.WriteFile(filepath.Join(base, "release-"+winner), nil, 0600)
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err = os.Stat(filepath.Join(base, "acquired-"+loser))
		if err == nil {
			break
		}
		if !os.IsNotExist(err) || time.Now().After(deadline) {
			t.Fatal("cross-process release did not wake quota waiter", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	err = os.WriteFile(filepath.Join(base, "release-"+loser), nil, 0600)
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		err = child.Wait()
		if err != nil {
			t.Fatal("quota process failed", err)
		}
	}
	status, err := s.Status(t.Context())
	if err != nil || status.Artifacts["writing"].Count != 0 {
		t.Fatal("processes leaked reservations", status, err)
	}
}

func quotaChild(t *testing.T, base string) {
	t.Helper()
	db, err := OpenStore(base, os.Getenv("GEKTOR_QUOTA_WORK"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	writer, err := db.NewRun()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	name := os.Getenv("GEKTOR_QUOTA_NAME")
	err = os.WriteFile(filepath.Join(base, "ready-"+name), nil, 0600)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	limit, err := writer.reserveArtifact(ctx, "temporary")
	if err != nil || limit != MaxArtifactBytes {
		t.Fatal("incorrect cross-process reservation", limit, err)
	}
	err = os.WriteFile(filepath.Join(base, "acquired-"+name), nil, 0600)
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, err = os.Stat(filepath.Join(base, "release-"+name))
		if err == nil {
			break
		}
		if !os.IsNotExist(err) || ctx.Err() != nil {
			t.Fatal("release signal missing", err, ctx.Err())
		}
		time.Sleep(10 * time.Millisecond)
	}
	err = writer.discardArtifact("temporary")
	if err != nil {
		t.Fatal(err)
	}
}
