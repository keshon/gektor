package filetxn

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceRestoresEarlierFilesAfterCommitFailure(t *testing.T) {
	root := t.TempDir()
	a, b, c := filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "c")
	for _, path := range []string{a, c} {
		if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var writes []Write
	for _, path := range []string{a, b, c} {
		writes = append(writes, Write{Path: path, Save: func(staged string) error {
			if filepath.Dir(staged) != root {
				t.Fatal("serializer changed output directory")
			}
			return os.WriteFile(staged, []byte("changed"), 0600)
		}})
	}
	failure := errors.New("commit denied")
	err := replace(writes, func(from, to string) error {
		if to == c && strings.HasSuffix(from, ".new") {
			return failure
		}
		return os.Rename(from, to)
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	for _, path := range []string{a, c} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "original" {
			t.Fatalf("original lost: %s %q %v", path, data, err)
		}
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Fatal("new target survived rollback", err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 2 {
		t.Fatal("staging files leaked", entries)
	}
}

func TestReplaceStagesAllFilesBeforePublishing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "existing")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	err := Replace([]Write{
		{Path: target, Save: func(path string) error { return os.WriteFile(path, []byte("changed"), 0600) }},
		{Path: filepath.Join(root, "bad"), Save: func(string) error { return errors.New("disk full") }},
	})
	data, _ := os.ReadFile(target)
	if err == nil || string(data) != "original" {
		t.Fatalf("failed staging changed target: %q %v", data, err)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatal("staging files leaked", entries)
	}
}

func TestReplaceRetainsBackupWhenRollbackFails(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "existing")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	err := replace([]Write{{Path: target, Save: func(path string) error { return os.WriteFile(path, []byte("changed"), 0600) }}}, func(from, to string) error {
		if to == target {
			return errors.New("target unavailable")
		}
		return os.Rename(from, to)
	})
	if err == nil || !strings.Contains(err.Error(), "backup in") {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(filepath.Join(root, ".settings-*", "original"))
	if len(backups) != 1 {
		t.Fatal("recovery backup removed", backups)
	}
	data, _ := os.ReadFile(backups[0])
	if string(data) != "original" {
		t.Fatal("backup corrupted")
	}
}
