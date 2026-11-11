package workspace

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestObserverMeasuresActualEffects(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0600)
	observe := ws.NewChangeObserver()
	err = observe.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0600)
	if paths, err := observe.Observe(t.Context()); err != nil || len(paths) != 0 {
		t.Fatalf("same bytes counted: %v %v", paths, err)
	}
	os.Remove(filepath.Join(root, "a.txt"))
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0600)
	if paths, err := observe.Observe(t.Context()); err != nil || !reflect.DeepEqual(paths, []string{"a.txt", "b.txt"}) {
		t.Fatalf("effects=%v %v", paths, err)
	}
}

func TestFinalObservationSurvivesRunCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	ws, err := New(t.TempDir())
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	observe := ws.NewChangeObserver()
	err = observe.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Root(), "changed.txt"), []byte("saved before cancellation"), 0600); err != nil {
		t.Fatal(err)
	}
	cancel()
	paths, err := observe.Observe(ctx)
	if err != nil || !reflect.DeepEqual(paths, []string{"changed.txt"}) {
		t.Fatalf("paths=%v err=%v", paths, err)
	}
}

func TestObserverRequiresSuccessfulBeginForEachRun(t *testing.T) {
	ws, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	observer := ws.NewChangeObserver()
	if _, err := observer.Observe(t.Context()); err == nil {
		t.Fatal("observation accepted before Begin")
	}
	if err := observer.Begin(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("first.txt", []byte("first run"), 0600); err != nil {
		t.Fatal(err)
	}
	// A fresh run starts from current files, not the preceding run's baseline.
	if err := observer.Begin(t.Context()); err != nil {
		t.Fatal(err)
	}
	if paths, err := observer.Observe(t.Context()); err != nil || len(paths) != 0 {
		t.Fatalf("previous run leaked into new baseline: %v %v", paths, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := observer.Begin(ctx); err == nil {
		t.Fatal("cancelled Begin succeeded")
	}
	if _, err := observer.Observe(t.Context()); err == nil {
		t.Fatal("failed Begin retained an active baseline")
	}
}

func TestFailedObservationPreservesBaseline(t *testing.T) {
	root := t.TempDir()
	ws, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	observer := ws.NewChangeObserver()
	if err := observer.Begin(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteFile("changed.txt", []byte("retained effect"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.Observe(t.Context()); err == nil {
		t.Fatal("scan through closed workspace succeeded")
	}
	ws.files, err = os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := observer.Observe(t.Context())
	if err != nil || !reflect.DeepEqual(paths, []string{"changed.txt"}) {
		t.Fatalf("failed scan discarded baseline: %v %v", paths, err)
	}
}
