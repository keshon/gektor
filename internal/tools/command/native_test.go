package command

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeArchiveStartDiagnostic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "library.exe")
	err := os.WriteFile(path, []byte("!<arch>\n"), 0700)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{path, "." + string(filepath.Separator) + "library.exe"}
	if runtime.GOOS == "windows" {
		paths = append(paths, `.\library`)
	}
	for _, program := range paths {
		t.Run(program, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), program)
			output, err := Result(t.Context(), cmd, dir, nil)
			if err == nil || !strings.Contains(err.Error(), "file is an ar archive, not an executable; check the build target") {
				t.Fatalf("missing archive diagnosis: output=%q, error=%v", output, err)
			}
			var pathErr *os.PathError
			if !errors.As(err, &pathErr) || !errors.Is(err, pathErr.Err) || pathErr.Op != "fork/exec" {
				t.Fatalf("lost operating-system failure: %v", err)
			}
			if cmd.Process != nil || !strings.Contains(err.Error(), "process did not start:") {
				t.Fatalf("changed start-failure classification: %v", err)
			}
		})
	}
}

func TestNativeStartDiagnosticPreservesOtherErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unknown.exe")
	err := os.WriteFile(path, []byte("not an executable\n"), 0700)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), path)
	_, err = Execute(t.Context(), cmd, dir, nil)
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || err != pathErr {
		t.Fatalf("nonarchive start error changed: %v", err)
	}

	err = os.WriteFile(path, []byte("!<arch>\n"), 0700)
	if err != nil {
		t.Fatal(err)
	}
	lookupErr := &exec.Error{Name: "unknown.exe", Err: exec.ErrNotFound}
	if got := diagnoseStartError(cmd, lookupErr); got != lookupErr {
		t.Fatalf("lookup failure inspected an unrelated file: %v", got)
	}
	otherErr := errors.New("start unavailable")
	if got := diagnoseStartError(cmd, otherErr); got != otherErr {
		t.Fatalf("unknown error changed: %v", got)
	}
}

func TestNativeStartDiagnosticUsesResolvedPATH(t *testing.T) {
	searchDir, workDir := t.TempDir(), t.TempDir()
	name := "native-start-probe.exe"
	err := os.WriteFile(filepath.Join(searchDir, name), []byte("not an executable\n"), 0700)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(workDir, name), []byte("!<arch>\n"), 0700)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", searchDir)
	cmd := exec.CommandContext(t.Context(), name)
	_, err = Execute(t.Context(), cmd, workDir, nil)
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || err != pathErr {
		t.Fatalf("diagnosed the workspace namesake instead of PATH executable: %v", err)
	}
	if pathErr.Path != filepath.Join(searchDir, name) {
		t.Fatalf("unexpected executed path: %q", pathErr.Path)
	}
}
