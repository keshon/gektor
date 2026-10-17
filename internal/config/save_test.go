package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Saving the same settings twice must update the file, not fail or leave a
// second copy: the settings form stages a profile and can save it again
// after further edits.
func TestSaveCustomUpdatesTheSameFile(t *testing.T) {
	dir := t.TempDir()
	first := valid()
	first.Identity = &Identity{UserName: "Alex"}

	path, err := SaveCustom(first, dir, "")
	if err != nil {
		t.Fatalf("SaveCustom: %v", err)
	}
	if filepath.Base(path)[:7] != "custom-" {
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

// The custom directory is the only place a save may overwrite. A source
// outside it means "create a new one", so a bundled profile is never
// rewritten by editing in the UI.
func TestSaveCustomNeverOverwritesOutsideTheCustomDirectory(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "llama-external.json")
	bundled := valid()
	if err := SaveCopy(bundled, outside); err != nil {
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