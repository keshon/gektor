package session

import (
	"path/filepath"
	"testing"
)

func TestWorkspaceRoundtripAndUnavailable(t *testing.T) {
	root := t.TempDir()
	s := New(nil)
	s.Workspace = root
	path := filepath.Join(t.TempDir(), "state.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := got.ResolveWorkspace(); err != nil || resolved != root {
		t.Fatalf("resolved %q: %v", resolved, err)
	}
	got.Workspace = filepath.Join(root, "missing")
	if _, err := got.ResolveWorkspace(); err == nil {
		t.Fatal("missing workspace accepted")
	}
	got.Workspace = "."
	if _, err := got.ResolveWorkspace(); err == nil {
		t.Fatal("relative workspace accepted")
	}
}
func TestLegacyWorkspaceUnassigned(t *testing.T) {
	for _, raw := range []string{`[]`, `{"version":1,"history":[]}`} {
		s, err := Decode([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if s.Workspace != "" {
			t.Fatal("legacy workspace inferred")
		}
		if _, err := s.ResolveWorkspace(); err == nil {
			t.Fatal("unassigned workspace accepted")
		}
	}
}
