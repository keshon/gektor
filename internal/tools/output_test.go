package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/keshon/tars/internal/tars"
)

// Spilled tool output lands in the user's TARS directory, never in the
// workspace the agent is working on. A 4 MB fetch written into the repo
// is a file the agent did not make, in a tree the user is reviewing.
func TestSpill_WritesFullContentOutsideTheWorkspace(t *testing.T) {
	content := strings.Repeat("x", 100)
	p := Spill("run_shell", content)
	if p == "" {
		t.Fatal("expected path, got empty")
	}
	t.Cleanup(func() { os.Remove(p) })

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read spill: %v", err)
	}
	if string(data) != content {
		t.Fatal("spill content mismatch")
	}
	if !strings.HasPrefix(p, tars.Sub("output")) {
		t.Fatalf("spill landed outside the user's TARS directory: %s", p)
	}
}

func TestSpill_EmptyContentSpillsNothing(t *testing.T) {
	if p := Spill("run_shell", ""); p != "" {
		os.Remove(p)
		t.Fatalf("empty content must not spill, got %s", p)
	}
}
