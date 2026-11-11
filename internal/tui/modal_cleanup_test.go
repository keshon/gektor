package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/config"
)

func TestModalCleanupPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("optional rendered fixtures")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		m := sizeModelTo(t, settingsModel(t), 100, 32)
		m.applyTheme(theme)
		m.openTheme()
		if err := os.WriteFile(filepath.Join(dir, "modal-theme-"+theme+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
		m.focusForm(0)
		m.chrome.Dialog.Form.OpenPicker(m.settingsProps())
		if err := os.WriteFile(filepath.Join(dir, "modal-picker-"+theme+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
		m.closeDialog()
		m.chrome.Open("Long prompt", []string{strings.Repeat("A long prompt with enough content to scroll.\n", 70)})
		if err := os.WriteFile(filepath.Join(dir, "modal-scroll-"+theme+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
