package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveCustomUpdatesTheSameFile(t *testing.T) {
	dir := t.TempDir()
	first := validProfile()
	first.Identity = &Identity{UserName: "Alex"}

	path, err := SaveCustom(first, dir, "")
	if err != nil {
		t.Fatalf("SaveCustom: %v", err)
	}

	name := filepath.Base(path)
	if !strings.HasPrefix(name, "custom-") {
		t.Fatalf("target is not a custom profile: %s", path)
	}

	second := first
	second.Identity = &Identity{UserName: "Robin"}
	same, err := SaveCustom(second, dir, path)
	if err != nil {
		t.Fatalf("second SaveCustom: %v", err)
	}

	if same != path {
		t.Fatalf("resave chose a new file: %s vs %s", same, path)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if reloaded.Identity == nil || reloaded.Identity.UserName != "Robin" {
		t.Fatalf("resave did not land: %+v", reloaded.Identity)
	}
}

func TestSaveCustomNeverOverwritesOutsideTheCustomDirectory(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "llama-external.json")
	bundled := validProfile()
	err := SaveCopy(bundled, outside)
	if err != nil {
		t.Fatal(err)
	}

	custom := t.TempDir()
	fresh, err := SaveCustom(bundled, custom, outside)
	if err != nil {
		t.Fatal(err)
	}

	if fresh == outside {
		t.Fatal("save reused a profile outside the custom directory")
	}

	original, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(original), `"local"`) {
		t.Fatalf("bundled file was modified: %s", original)
	}
}

func TestSaveCopyNeverReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	original := []byte("existing profile")
	err := os.WriteFile(path, original, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	profile := validProfile()
	err = SaveCopy(profile, path)
	if err == nil {
		t.Fatal("replaced an existing profile")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("existing profile changed: %s", got)
	}
}

func TestInvalidCustomProfileKeepsSavedFile(t *testing.T) {
	directory := t.TempDir()
	profile := validProfile()
	path, err := SaveCustom(profile, directory, "")
	if err != nil {
		t.Fatal(err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	profile.Version = 0
	_, err = SaveCustom(profile, directory, path)
	if err == nil {
		t.Fatal("saved an invalid custom profile")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed save changed the custom profile")
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("failed save left temporary files: %v", entries)
	}
}
