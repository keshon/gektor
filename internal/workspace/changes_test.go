package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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
func TestFileSetHonorsGitIgnoreAndTrackedExceptions(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git: %v %s", err, out)
	}
	os.MkdirAll(filepath.Join(root, "build"), 0755)
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n*.log\n"), 0600)
	os.WriteFile(filepath.Join(root, "build", "source.go"), []byte("package fixture"), 0600)
	exec.Command("git", "-C", root, "add", "-f", "build/source.go").Run()
	os.WriteFile(filepath.Join(root, "build", "artifact"), []byte("generated"), 0600)
	os.WriteFile(filepath.Join(root, "secret.log"), []byte("ignored"), 0600)
	ws, _ := New(root)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	files, err := ws.FileSet(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !files[filepath.Join("build", "source.go")] || files[filepath.Join("build", "artifact")] || files["secret.log"] {
		t.Fatalf("files=%v", files)
	}
}
func TestInstructionsLoadAncestorsBeforeRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	os.MkdirAll(root, 0755)
	os.WriteFile(filepath.Join(parent, "AGENTS.md"), []byte("outer rule"), 0600)
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("inner rule"), 0600)
	ws, err := New(root)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	text := ws.Instructions()
	if strings.Index(text, "outer rule") < 0 || strings.Index(text, "inner rule") < strings.Index(text, "outer rule") {
		t.Fatal(text)
	}
}

func TestStateDirectoriesStayOutOfGitInventory(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git: %v %s", err, out)
	}
	path := filepath.Join(root, ".gektor", "tasks")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "history.json"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	ws, err := New(root)
	if ws != nil {
		t.Cleanup(func() { ws.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	files, err := ws.FileSet(t.Context())
	if err != nil || len(files) != 0 {
		t.Fatalf("state included: %v %v", files, err)
	}
	observe, err := ws.ChangeObserver(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "history.json"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if paths, err := observe(); err != nil || len(paths) != 0 {
		t.Fatalf("state effects: %v %v", paths, err)
	}
}
