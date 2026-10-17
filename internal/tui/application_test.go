package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/workspace"
)

// The fixture builds a saved task so the apply path has a checkpoint to
// read; requestProfileChange reads it before it writes anything, and a
// missing one would fail for a reason unrelated to the check.
func writePlannedSession(t *testing.T, dir, mode string, history []llm.Message) string {
	t.Helper()
	path := filepath.Join(dir, "state.json")
	if err := agent.SaveState(path, history); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mode"), []byte(mode), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Applying settings restarts the launch so the profile is composed into
// fresh services. The queued restart must carry the draft, the history and
// the session directory, and must not have started a run.
func TestSettingsApplyQueuesRestartWithContextAndDraft(t *testing.T) {
	m := settingsModel(t)
	before, err := os.ReadFile(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	m.editSettings("model")
	if m.dialog == nil || m.dialog.form.Workspace.Section != "model" {
		t.Fatal("model editor unavailable")
	}
	focusSetting(m, "model")
	m.dialog.form.Editor.SetValue("another-model")
	m.applyForm()
	if m.dialog != nil {
		t.Fatalf("apply refused: %q", m.dialog.form.Error)
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
	if m.state != stDone {
		t.Fatal("apply started a run")
	}
	after, err := os.ReadFile(m.stateFile)
	if err != nil || string(after) != string(before) {
		t.Fatal("apply rewrote the saved task before the restart landed")
	}
}

// The Application page keeps staged values from another page in the same
// dialog, and always carries the session directory into the restart.
func TestApplicationPageRetainsStagedModelAndSession(t *testing.T) {
	m := settingsModel(t)
	original := m.sessionDirectory()
	m.application.Preferences.Workspace = original
	m.editSettings("model")
	focusSetting(m, "model")
	m.dialog.form.Editor.SetValue("selected-model")
	m.editSettings("application")
	focusSetting(m, "app-workspace")
	m.dialog.form.Editor.SetValue(t.TempDir())
	m.applyForm()
	if m.dialog != nil {
		t.Fatalf("apply refused: %q", m.dialog.form.Error)
	}
	if m.modeChange == nil {
		t.Fatal("apply did not queue a restart")
	}
	if m.modeChange.Preferences.Profile.Provider.Model != "selected-model" {
		t.Fatal("application page lost the staged model")
	}
	if m.modeChange.Directory != original {
		t.Fatalf("restart moved the session: %q -> %q", original, m.modeChange.Directory)
	}
}

// The workspace is resolved and checked, so the relaunch comes up rooted at
// a directory that exists rather than at whatever the editor string said.
func TestApplicationPageRejectsAMissingWorkspace(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("application")
	focusSetting(m, "app-workspace")
	m.dialog.form.Editor.SetValue(filepath.Join(t.TempDir(), "does-not-exist"))
	m.applyForm()
	if m.dialog == nil || m.dialog.form.Error == "" {
		t.Fatal("a missing workspace was accepted")
	}
	if m.modeChange != nil {
		t.Fatal("a refused apply still queued a restart")
	}
}

func TestApplicationPreview(t *testing.T) {
	dir := os.Getenv("TARS_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview opt-in")
	}
	os.MkdirAll(dir, 0700)
	m := settingsModel(t)
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.openApplication()
	if err := os.WriteFile(filepath.Join(dir, "application-mode.txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSharedSettingsPreview(t *testing.T) {
	dir := os.Getenv("TARS_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview export")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m := settingsModel(t)
	for _, page := range []string{"model", "application", "profiles"} {
		m.editSettings(page)
		if err := os.WriteFile(filepath.Join(dir, "shared-"+page+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.closeDialog()
	m.header = headerState{global: true, focused: true, menu: "settings"}
	if err := os.WriteFile(filepath.Join(dir, "shared-agent-menu.txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
		t.Fatal(err)
	}
}

// A task keeps the directory it already had, so resuming does not relocate
// a session. A task with no state file gets a directory of its own under
// the session root.
func TestAgentTaskKeepsTheDirectoryItAlreadyHas(t *testing.T) {
	m := sizeModel(t, testModel())
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	m.ws = ws
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "agent", Workspace: root}}
	m.config = &app.Services{Directory: root, Profile: config.Profile{Version: 1}}

	m.stateFile = ""
	if got, want := m.taskDirectory("abcd1234"), filepath.Join(m.sessionRoot(), "abcd1234"); got != want {
		t.Fatalf("new task directory = %q, want %q", got, want)
	}

	taskDir := filepath.Join(m.sessionRoot(), "agent-session")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m.stateFile = filepath.Join(taskDir, "state.json")
	if got := m.taskDirectory("abcd1234"); got != taskDir {
		t.Fatalf("resumed task moved to %q, want %q", got, taskDir)
	}
}

// Plan mode lives in a file beside the task, so the session list has to
// read it back rather than inferring it from the absence of a marker.
func TestSessionScanRestoresPlanMode(t *testing.T) {
	m := settingsModel(t)
	dir := m.sessionDirectory()
	m.stateFile = writePlannedSession(t, dir, "plan", []llm.Message{{Role: llm.RoleUser, Content: "question"}, {Role: llm.RoleAssistant, Content: "answer"}})
	if err := m.saveSessionSettings(); err != nil {
		t.Fatal(err)
	}
	entries := listSessionsIn(filepath.Dir(dir))
	if len(entries) != 1 || entries[0].mode != "Plan" {
		t.Fatalf("duplicated or wrong session mode: %+v", entries)
	}
	m.sessions = &sessionsState{entries: entries}
	_, handled := m.openApplicationSession(entries[0])
	if !handled || m.modeChange == nil || m.modeChange.Directory != dir || len(m.modeChange.History) != 2 {
		t.Fatal("saved session failed to restore")
	}
}

func TestAgentSeedReplacesLeadingSystemWithoutDroppingDialogue(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleSystem, Content: "old instructions"}, {Role: llm.RoleUser, Content: "hello"}, {Role: llm.RoleAssistant, Content: "hi"}}
	seed := agentSeed(history)
	if len(seed) != 3 || seed[0].Role != llm.RoleSystem || seed[0].Content != "" || seed[1].Content != "hello" || history[0].Content != "old instructions" {
		t.Fatal("competing prompt or mutated archive")
	}
}

// Opening a settings page and applying it without touching anything must
// not restart the launch: the reader did not ask for one.
func TestUnchangedSettingsDoNotRestart(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("model")
	m.applyForm()
	if m.modeChange != nil || m.dialog == nil {
		t.Fatal("an unchanged draft restarted the launch")
	}
}