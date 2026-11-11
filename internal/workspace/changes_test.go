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
	observe, err := ws.ChangeObserver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0600)
	if paths, err := observe(); err != nil || len(paths) != 0 {
		t.Fatalf("same bytes counted: %v %v", paths, err)
	}
	os.Remove(filepath.Join(root, "a.txt"))
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0600)
	if paths, err := observe(); err != nil || !reflect.DeepEqual(paths, []string{"a.txt", "b.txt"}) {
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
	observe, err := ws.ChangeObserver(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.Root(), "changed.txt"), []byte("saved before cancellation"), 0600); err != nil {
		t.Fatal(err)
	}
	cancel()
	paths, err := observe()
	if err != nil || !reflect.DeepEqual(paths, []string{"changed.txt"}) {
		t.Fatalf("paths=%v err=%v", paths, err)
	}
}
