package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestPersistenceScopesAndRevisions(t *testing.T) {
	dir, projectA, projectB := t.TempDir(), t.TempDir(), t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	project, created, err := store.Add(ctx, projectA, Input{Scope: Project, Text: "Keep tool calls in one row", SourceSession: "session1", SourceMessage: "message2"})
	if err != nil || !created {
		t.Fatalf("save: %+v %v", project, err)
	}
	duplicate, created, err := store.Add(ctx, projectA, Input{Scope: Project, Text: project.Text})
	if err != nil || created || duplicate.ID != project.ID {
		t.Fatalf("dedup: %+v %v", duplicate, err)
	}
	user, _, err := store.Add(ctx, "", Input{Scope: User, Text: "Be concise"})
	if err != nil {
		t.Fatal(err)
	}
	err = store.Close()
	if err != nil {
		t.Fatal(err)
	}
	store, err = OpenExisting(dir)
	if err != nil || store == nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	a, err := store.List(ctx, projectA, "")
	if err != nil || len(a) != 2 || a[1].ID != project.ID || a[1].SourceSession != "session1" {
		t.Fatalf("project recall: %+v %v", a, err)
	}
	b, err := store.List(ctx, projectB, "")
	if err != nil || len(b) != 1 || b[0].ID != user.ID {
		t.Fatalf("project leak: %+v %v", b, err)
	}
	err = store.Delete(ctx, projectB, project.ID, project.Revision)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project delete: %v", err)
	}
	_, err = store.Update(ctx, projectB, project.ID, project.Revision, Input{Scope: User, Text: "leaked"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-project move: %v", err)
	}
	edited, err := store.Update(ctx, projectA, project.ID, project.Revision, Input{Scope: Project, Text: "Keep compact tool rows"})
	if err != nil || edited.ProjectID != project.ProjectID || edited.Revision != 2 {
		t.Fatalf("edit: %+v %v", edited, err)
	}
	err = store.Delete(ctx, projectA, project.ID, project.Revision)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	err = store.Delete(ctx, projectA, edited.ID, edited.Revision)
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := store.List(ctx, projectA, "")
	if err != nil || len(remaining) != 1 {
		t.Fatalf("deleted recall: %+v %v", remaining, err)
	}
	_, _, err = store.Add(ctx, "", Input{Scope: Project, Text: "No workspace"})
	if err == nil {
		t.Fatal("accepted unscoped project memory")
	}
	files, err := os.ReadDir(projectA)
	if err != nil || len(files) != 0 {
		t.Fatalf("memory wrote project files: %v %v", files, err)
	}
}

func TestConcurrentWritersDoNotLoseEntriesOrOverwriteRevisions(t *testing.T) {
	dir := t.TempDir()
	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	ctx := t.Context()
	entry, _, err := first.Add(ctx, "", Input{Scope: User, Text: "original"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, store := range []*Store{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			text := "first"
			if i == 1 {
				text = "second"
			}
			_, updateErr := store.Update(ctx, "", entry.ID, entry.Revision, Input{Scope: User, Text: text})
			results <- updateErr
		}()
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for updateErr := range results {
		if updateErr == nil {
			success++
			continue
		}
		if errors.Is(updateErr, ErrConflict) {
			conflicts++
			continue
		}
		t.Fatal(updateErr)
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("updates: success=%d conflicts=%d", success, conflicts)
	}
	results = make(chan error, 2)
	for _, store := range []*Store{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, saveErr := store.Add(ctx, "", Input{Scope: User, Text: "same new fact"})
			results <- saveErr
		}()
	}
	wg.Wait()
	close(results)
	for saveErr := range results {
		if saveErr != nil {
			t.Fatal(saveErr)
		}
	}
	entries, err := first.List(ctx, "", "")
	if err != nil || len(entries) != 2 {
		t.Fatalf("concurrent dedup: %+v %v", entries, err)
	}
}

func TestSnapshotBoundedWholeEntriesAndLocalSearch(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	for _, text := range []string{strings.Repeat("界", 1000), "Tiny useful fact", "Another fact"} {
		_, _, err = store.Add(ctx, "", Input{Scope: User, Text: text})
		if err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := store.Snapshot(ctx, "", 500)
	if err != nil || snapshot.Omitted != 1 || len(snapshot.Entries) != 2 || utf8.RuneCountInString(snapshot.Text) > 500 {
		t.Fatalf("snapshot: %+v %v", snapshot, err)
	}
	if strings.Contains(snapshot.Text, "界") || !strings.Contains(snapshot.Text, "Tiny useful fact") {
		t.Fatal("partial or missing entry")
	}
	again, err := store.Snapshot(ctx, "", 500)
	if err != nil || snapshot.Text != again.Text {
		t.Fatal("snapshot not stable")
	}
	found, err := store.List(ctx, "", "USEFUL")
	if err != nil || len(found) != 1 || found[0].Text != "Tiny useful fact" {
		t.Fatalf("search: %+v %v", found, err)
	}
}

func TestReconnectMovedProjectKeepsIdentityAndRejectsMerges(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base := t.TempDir()
	oldPath, newPath := filepath.Join(base, "old"), filepath.Join(base, "new")
	err = os.Mkdir(oldPath, 0700)
	if err != nil {
		t.Fatal(err)
	}
	entry, _, err := store.Add(t.Context(), oldPath, Input{Scope: Project, Text: "project only"})
	if err != nil {
		t.Fatal(err)
	}
	err = store.Reconnect(t.Context(), oldPath, t.TempDir())
	if err == nil {
		t.Fatal("linked existing project")
	}
	err = os.Rename(oldPath, newPath)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Reconnect(t.Context(), oldPath, newPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(t.Context(), newPath, "")
	if err != nil || len(entries) != 1 || entries[0].ID != entry.ID || entries[0].ProjectID != entry.ProjectID {
		t.Fatalf("reconnected: %+v %v", entries, err)
	}
	other := t.TempDir()
	_, _, err = store.Add(t.Context(), other, Input{Scope: Project, Text: "other"})
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(base, "moved")
	err = os.Rename(newPath, moved)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Reconnect(t.Context(), newPath, other)
	if err == nil {
		t.Fatal("merged projects")
	}
}

func TestAbsentReadDoesNotCreateStorage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	store, err := OpenExisting(dir)
	if err != nil || store != nil {
		t.Fatalf("empty read: %v %v", store, err)
	}
	_, err = os.Stat(dir)
	if !os.IsNotExist(err) {
		t.Fatalf("created directory: %v", err)
	}
	_, err = Open("relative")
	if err == nil {
		t.Fatal("accepted relative storage")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store, err = Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, _, err = store.Add(ctx, "", Input{Scope: User, Text: "cancelled"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestScopeMovesAndDeletionPersistAcrossConnections(t *testing.T) {
	dir, firstProject, secondProject := t.TempDir(), t.TempDir(), t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	entry, _, err := store.Add(t.Context(), firstProject, Input{Scope: Project, Text: "Use concise explanations"})
	if err != nil {
		t.Fatal(err)
	}
	entry, err = store.Update(t.Context(), firstProject, entry.ID, entry.Revision, Input{Scope: User, Text: entry.Text})
	if err != nil || entry.ProjectID != "" {
		t.Fatalf("move to user: %+v %v", entry, err)
	}
	visible, err := store.List(t.Context(), secondProject, "")
	if err != nil || len(visible) != 1 {
		t.Fatalf("user not shared: %+v %v", visible, err)
	}
	entry, err = store.Update(t.Context(), secondProject, entry.ID, entry.Revision, Input{Scope: Project, Text: entry.Text})
	if err != nil {
		t.Fatal(err)
	}
	visible, err = store.List(t.Context(), firstProject, "")
	if err != nil || len(visible) != 0 {
		t.Fatalf("moved entry leaked: %+v %v", visible, err)
	}
	err = store.Delete(t.Context(), secondProject, entry.ID, entry.Revision)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Close()
	if err != nil {
		t.Fatal(err)
	}
	store, err = OpenExisting(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := store.Snapshot(t.Context(), secondProject, DefaultBudget)
	if err != nil || snapshot.Text != "" || snapshot.Omitted != 0 {
		t.Fatalf("deleted entry recalled: %+v %v", snapshot, err)
	}
}
