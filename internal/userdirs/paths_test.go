package userdirs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSub_IsUnderTheUserDirectory(t *testing.T) {
	directory := home(t)
	got := Sub("tasks", "abc123")
	want := filepath.Join(directory, DirName, "tasks", "abc123")
	if got != want {
		t.Fatalf("Sub = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, directory) {
		t.Fatalf("user data escaped the home directory: %q", got)
	}
}

func TestSub_CreatesNothing(t *testing.T) {
	got := Sub("output", "never-written.log")
	directory := filepath.Dir(got)
	_, err := os.Stat(directory)
	if err == nil {
		t.Fatalf("Sub must not create directories on its own: %q", directory)
	}
}

func home(t *testing.T) string {
	t.Helper()

	directory, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	return directory
}
