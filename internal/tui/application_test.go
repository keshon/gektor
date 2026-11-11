package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/screen"
)

// Applying settings restarts the launch so the profile is composed into
// fresh services. The queued restart must carry the draft, the history and
// the session directory, and must not have started a run.
func TestSettingsApplyQueuesRestartWithContextAndDraft(t *testing.T) {
	m := settingsModel(t)
	before, err := os.ReadFile(stateFileOf(m))
	if err != nil {
		t.Fatal(err)
	}
	m.editSettings("model")
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form.EditSession.Section != "model" {
		t.Fatal("model editor unavailable")
	}
	focusSetting(m, "model")
	m.chrome.Dialog.Form.Editor.SetValue("another-model")
	m.applyForm()
	if m.chrome.Dialog != nil {
		t.Fatalf("apply refused: %q", m.chrome.Dialog.Form.Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply did not queue a restart")
	}
	if m.modeChange.Preferences.Profile.Provider.Model != "another-model" {
		t.Fatalf("model = %q", m.modeChange.Preferences.Profile.Provider.Model)
	}
	if m.modeChange.Draft != "unfinished draft" {
		t.Fatalf("draft = %q", m.modeChange.Draft)
	}
	if len(m.modeChange.History) != 3 {
		t.Fatalf("history carried %d messages, want 3", len(m.modeChange.History))
	}
	if !m.agent.Status().Idle {
		t.Fatal("apply started a run")
	}
	after, err := os.ReadFile(stateFileOf(m))
	if err != nil || string(after) != string(before) {
		t.Fatal("apply rewrote the saved task before the restart landed")
	}
}

// The Workspace page keeps staged values from another page in the same
// dialog, and always carries the session directory into the restart.
func TestWorkspacePageRetainsStagedModelAndSession(t *testing.T) {
	m := settingsModel(t)
	original := snapshotOf(m).Directory
	m.application.Preferences.Workspace = original
	m.editSettings("model")
	focusSetting(m, "model")
	m.chrome.Dialog.Form.Editor.SetValue("selected-model")
	m.editSettings("workspace")
	focusSetting(m, "app-workspace")
	m.chrome.Dialog.Form.Editor.SetValue(t.TempDir())
	m.applyForm()
	if m.chrome.Dialog != nil {
		t.Fatalf("apply refused: %q", m.chrome.Dialog.Form.Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply did not queue a restart")
	}
	if m.modeChange.Preferences.Profile.Provider.Model != "selected-model" {
		t.Fatal("workspace page lost the staged model")
	}
	if m.modeChange.Directory != original {
		t.Fatalf("restart moved the session: %q -> %q", original, m.modeChange.Directory)
	}
}

// The workspace is resolved and checked, so the relaunch comes up rooted at
// a directory that exists rather than at whatever the editor string said.
func TestWorkspacePageRejectsAMissingWorkspace(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("workspace")
	focusSetting(m, "app-workspace")
	m.chrome.Dialog.Form.Editor.SetValue(filepath.Join(t.TempDir(), "does-not-exist"))
	m.applyForm()
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form.Error == "" {
		t.Fatal("a missing workspace was accepted")
	}
	if m.modeChange != nil {
		t.Fatal("a refused apply still queued a restart")
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

// Opening a settings page and applying it without touching anything must
// not restart the launch: the reader did not ask for one.
func TestUnchangedSettingsDoNotRestart(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("model")
	m.applyForm()
	if m.modeChange != nil || m.chrome.Dialog == nil {
		t.Fatal("an unchanged draft restarted the launch")
	}
}
