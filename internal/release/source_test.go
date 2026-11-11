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
	runTestGit(t, root, "init", "--quiet")
	modulePath := filepath.Join(root, "go.mod")
	module := []byte("module github.com/keshon/gektor\n")
	err := os.WriteFile(modulePath, module, 0644)
	if err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "go.mod")
	runTestGit(t, root, "-c", "user.name=Packaging Test", "-c", "user.email=packaging@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture")

	_, err = readSource(t.Context(), root, true)
	if err != nil {
		t.Fatal(err)
	}

	untracked := filepath.Join(root, "untracked.txt")
	err = os.WriteFile(untracked, []byte("unpublished"), 0644)
	if err != nil {
		t.Fatal(err)
	}
	options := Options{Root: root, Version: "v1.2.3"}
	_, err = Build(t.Context(), options)
	if err == nil || !strings.Contains(err.Error(), "clean checkout") {
		t.Fatal("dirty versioned build accepted", err)
	}

	output := filepath.Join(root, "dist")
	_, err = os.Stat(output)
	if !os.IsNotExist(err) {
		t.Fatal("rejected build touched output directory", err)
	}

	src, err := readSource(t.Context(), root, false)
	if err != nil || !src.dirty {
		t.Fatal("development source rejected", err)
	}
}

// runTestGit configures isolated fixture repositories, never the working checkout.
func runTestGit(t *testing.T, root string, args ...string) {
	t.Helper()

	commandArgs := append([]string{"-C", root}, args...)
	cmd := exec.CommandContext(t.Context(), "git", commandArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(string(output), err)
	}
}
