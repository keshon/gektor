//go:build ignore

// Run from the repository root with go run scripts/check-tui-package.go.
// This builds the public TUI packages and a separate workbench consumer.
package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const sourceModule = "github.com/keshon/gektor"

func main() {
	keep := flag.Bool("keep", false, "retain the temporary module snapshot and consumer")
	flag.Parse()
	if err := checkPackage(*keep); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func checkPackage(keep bool) error {
	module, err := os.ReadFile("go.mod")
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(module, []byte("module "+sourceModule+"\n")) && !bytes.HasPrefix(module, []byte("module "+sourceModule+"\r\n")) {
		return fmt.Errorf("run from the GEKTOR repository root")
	}
	root, err := os.MkdirTemp("", "gektor-controls-*")
	if err != nil {
		return err
	}
	if keep {
		fmt.Println("Consumer-check workspace:", root)
	} else {
		defer os.RemoveAll(root)
	}
	candidate := filepath.Join(root, "gektor")
	consumer := filepath.Join(root, "consumer")
	hash := sha256.New()
	for _, tree := range []struct{ from, to string }{
		{"pkg/tui/screen", filepath.Join(candidate, "pkg", "tui", "screen")},
		{"pkg/tui/components", filepath.Join(candidate, "pkg", "tui", "components")},
		{"pkg/tui/examples/workbench", consumer},
	} {
		err := copyPackageTree(tree.from, tree.to, hash)
		if err != nil {
			return err
		}
	}
	if err := writeCandidate(filepath.Join(candidate, "go.mod"), module); err != nil {
		return err
	}
	sums, err := os.ReadFile("go.sum")
	if err != nil {
		return err
	}
	if err := writeCandidate(filepath.Join(candidate, "go.sum"), sums); err != nil {
		return err
	}
	version := ""
	for _, line := range strings.Split(string(module), "\n") {
		if strings.HasPrefix(line, "go ") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "go "))
		}
	}
	consumerModule := fmt.Sprintf("module example.invalid/workbench-consumer\n\ngo %s\n\nrequire %s v0.0.0\n\nreplace %s => ../gektor\n", version, sourceModule, sourceModule)
	if err := writeCandidate(filepath.Join(consumer, "go.mod"), []byte(consumerModule)); err != nil {
		return err
	}
	fmt.Printf("Public TUI source SHA-256: %x\n", hash.Sum(nil))
	for _, dir := range []string{candidate, consumer} {
		for _, args := range [][]string{{"mod", "tidy"}, {"test", "-p", "1", "./..."}, {"vet", "./..."}, {"build", "./..."}} {
			cmd := exec.Command("go", args...)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOWORK=off")
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("%s: go %s: %w", filepath.Base(dir), strings.Join(args, " "), err)
			}
		}
	}
	fmt.Println("Public TUI packages and separate local consumer passed with unchanged imports; no released version was tested.")
	return nil
}

// Package assets, licenses and test fixtures travel with their Go source. This
// keeps go:embed inputs intact without interpreting platform-specific patterns.
func copyPackageTree(from, to string, digest io.Writer) error {
	return filepath.WalkDir(from, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("package snapshot requires a regular file: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".go") {
			err = checkImports(path, data)
			if err != nil {
				return err
			}
		}

		_, err = fmt.Fprintf(digest, "%s\x00%d\x00", filepath.ToSlash(path), len(data))
		if err != nil {
			return err
		}
		_, err = digest.Write(data)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		return writeCandidate(filepath.Join(to, relative), data)
	})
}

func checkImports(path string, data []byte) error {
	file, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ImportsOnly)
	if err != nil {
		return err
	}
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return err
		}
		if strings.HasPrefix(value, sourceModule+"/") && value != sourceModule+"/pkg/tui/screen" && !strings.HasPrefix(value, sourceModule+"/pkg/tui/components/") {
			return fmt.Errorf("%s imports application package %s", path, value)
		}
	}
	return nil
}

func writeCandidate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
