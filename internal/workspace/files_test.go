package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
