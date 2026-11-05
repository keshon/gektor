package session

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestChildPathsAreConfinedAndParentScoped(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "state.json")
	path := ChildStatePath(parent, `../../escape\bad`)
	rel, err := filepath.Rel(filepath.Dir(parent), path)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatal(path, err)
	}
	if path == ChildStatePath(filepath.Join(filepath.Dir(parent), "other.json"), `../../escape\bad`) {
		t.Fatal("parent collision")
	}
	if ChildStatePath("", "id") != "" || ChildStatePath(parent, "") != "" {
		t.Fatal("empty identity persisted")
	}
}
