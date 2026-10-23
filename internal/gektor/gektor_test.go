package gektor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Everything GEKTOR persists belongs under one dot-directory in the user's
// home. Splitting it across a workspace .gektor and an executable-relative
// var/ is what produced two session libraries that each looked complete.
func TestSub_IsUnderTheUserDirectory(t *testing.T) {
	got := Sub("tasks", "abc123")
	want := filepath.Join(home(t), DirName, "tasks", "abc123")
	if got != want {
		t.Fatalf("Sub = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, home(t)) {
		t.Fatalf("user data escaped the home directory: %q", got)
	}
}

func TestSub_CreatesNothing(t *testing.T) {
	got := Sub("output", "never-written.log")
	if _, err := os.Stat(filepath.Dir(got)); err == nil {
		t.Fatalf("Sub must not create directories on its own: %q", filepath.Dir(got))
	}
}

func home(t *testing.T) string {
	t.Helper()
	h, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	return h
}
