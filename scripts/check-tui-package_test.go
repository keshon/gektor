//go:build ignore

package main

import (
	"bytes"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPublicTUIRejectsApplicationImports(t *testing.T) {
	for _, path := range []string{
		sourceModule + "/internal/app",
		sourceModule + "/internal/tui/agent",
		sourceModule + "/pkg/tui/components-extra",
	} {
		data := []byte("package example\nimport _ " + `"` + path + `"`)
		if checkImports("example.go", data) == nil {
			t.Fatalf("public TUI admitted application import %s", path)
		}
	}
}

func TestPublicTUIAllowsControlAndExternalImports(t *testing.T) {
	data := []byte(`package example
import (
 "strings"
 tea "charm.land/bubbletea/v2"
 "github.com/keshon/gektor/pkg/tui/screen"
 "github.com/keshon/gektor/pkg/tui/components/fields"
)
`)
	if err := checkImports("example.go", data); err != nil {
		t.Fatal(err)
	}
}

func TestPackageSnapshotIncludesEmbeddedAssetsAndLicenses(t *testing.T) {
	source := t.TempDir()
	files := map[string]string{
		"go.mod":             "module example.invalid/embedded-fixture\n\ngo 1.26\n",
		"fixture.go":         "package fixture\nimport _ \"embed\"\n//go:embed assets/message.txt\nvar Message string\n",
		"assets/message.txt": "embedded content\n",
		"LICENSE":            "fixture license\n",
	}
	for name, data := range files {
		err := writeCandidate(filepath.Join(source, name), []byte(data))
		if err != nil {
			t.Fatal(err)
		}
	}
	candidate := t.TempDir()
	digest := sha256.New()
	err := copyPackageTree(source, candidate, digest)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(candidate, name))
		if err != nil || string(got) != want {
			t.Fatalf("snapshot lost %s: got %q, err %v", name, got, err)
		}
	}
	command := exec.CommandContext(t.Context(), "go", "test", "./...")
	command.Dir = candidate
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("embedded consumer did not build: %v\n%s", err, output)
	}

	previous := digest.Sum(nil)
	for _, name := range []string{"assets/message.txt", "LICENSE"} {
		err = os.WriteFile(filepath.Join(source, name), []byte("changed\n"), 0600)
		if err != nil {
			t.Fatal(err)
		}
		digest.Reset()
		err = copyPackageTree(source, candidate, digest)
		if err != nil {
			t.Fatal(err)
		}
		current := digest.Sum(nil)
		if bytes.Equal(current, previous) {
			t.Fatalf("snapshot digest ignored a change to %s", name)
		}
		previous = current
	}
}
