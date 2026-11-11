package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
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

func TestApplicationPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview opt-in")
	}
	os.MkdirAll(dir, 0700)
	m := settingsModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.openWorkspace()
	if err := os.WriteFile(filepath.Join(dir, "application-mode.txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSharedSettingsPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview export")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m := settingsModel(t)
	for _, page := range []string{"model", "workspace", "profiles"} {
		m.editSettings(page)
		if err := os.WriteFile(filepath.Join(dir, "shared-"+page+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.closeDialog()
	m.chrome.Header = screen.MenuState{Global: true, Focused: true, Menu: "settings"}
	if err := os.WriteFile(filepath.Join(dir, "shared-agent-menu.txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsLibrariesPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview opt-in")
	}
	os.MkdirAll(dir, 0700)
	m := settingsModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 34})
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.editSettings("profiles")
	focusSetting(m, "load-profile")
	m.chrome.Dialog.Form.OpenPicker(m.settingsProps())
	p := m.chrome.Dialog.Form.Picker
	m.Update(settings.ScanMsg{ID: p.ID, Items: []string{filepath.Join(dir, "custom.json")}})
	write("profile-picker")
	m.chrome.Dialog.Form.PickerKey(tea.KeyPressMsg{Code: tea.KeyEscape}, m.settingsProps())
	m.editSettings("prompts")
	write("prompts-page")
}

func TestMenuSettingsPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_SETTINGS_PREVIEW_DIR")
	if dir == "" {
		t.Skip("renderer export opt-in")
	}
	os.MkdirAll(dir, 0700)
	for _, name := range []string{"menu", "generation", "prompts", "delete"} {
		m := settingsModel(t)
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
		switch name {
		case "menu":
			m.chrome.Header.Global = true
			m.chrome.Header.Focused = true
			m.chrome.Header.Menu = "settings"
		case "delete":
			m.runAction("delete-current")
		default:
			m.editSettings(name)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSettingsWorkspacePreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview opt-in")
	}
	os.MkdirAll(dir, 0700)
	m := settingsModel(t)
	m.profile.Profile.Provider.URL = "http://127.0.0.1:8080"
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 34})
	m.editSettings("model")
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("model-page")
	m.editSettings("generation")
	write("generation-workspace")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	write("settings-narrow")
}
