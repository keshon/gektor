package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/keshon/gektor/internal/app"
)

// A startup failure must surface rather than starting a program. The
// failure is forced with a SessionDirectory that is a file rather than a
// directory. The Agent constructor validates it before starting a run.
func TestNewFailsOnUnwritableSessionDirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{PreparedProfile: &app.PreparedProfile{Directory: t.TempDir()}, SessionDirectory: blocker}
	if _, err := New(context.Background(), cfg); err == nil {
		t.Fatal("unwritable session directory accepted")
	}
}
