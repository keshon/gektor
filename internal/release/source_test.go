package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionedBuildRequiresCleanSource(t *testing.T) {
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(output), err)
		}
	}
	runGit("init", "--quiet")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/keshon/gektor\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "go.mod")
	runGit("-c", "user.name=Packaging Test", "-c", "user.email=packaging@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture")
	if _, err := readSource(t.Context(), root, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("unpublished"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(t.Context(), Options{Root: root, Version: "v1.2.3"}); err == nil || !strings.Contains(err.Error(), "clean checkout") {
		t.Fatal("dirty versioned build accepted", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist")); !os.IsNotExist(err) {
		t.Fatal("rejected build touched output directory", err)
	}
	src, err := readSource(t.Context(), root, false)
	if err != nil || !src.dirty {
		t.Fatal("development source rejected", err)
	}
}
