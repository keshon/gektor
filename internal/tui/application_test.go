package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/workspace"
)

func TestModeSwitchCarriesContextAndDraftWithoutRunning(t *testing.T) {
	m := settingsModel(t)
	profile := m.chat.config.Profile
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "chat", Voice: "off", Profile: &profile}}
	before := m.chat.current.History()
	m.openApplication()
	m.dialog.form.Editor.SetValue("agent")
	focusSetting(m, "app-workspace")
	m.dialog.form.Editor.SetValue(t.TempDir())
	m.applyForm()
	if m.modeChange == nil || m.modeChange.Preferences.Mode != "agent" || m.modeChange.Draft != "unfinished draft" || len(m.modeChange.History) != len(before) || m.state != stDone {
		t.Fatal("mode transition changed history, draft or ran a task")
	}
}
func TestDialogueProjectionDoesNotOfferHistoricalTools(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleSystem, Content: "old instructions"}, {Role: llm.RoleUser, Content: "inspect files"}, {Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "x", Name: "read_file"}}}, {Role: llm.RoleTool, Content: "file content", ToolCallID: "x"}, {Role: llm.RoleAssistant, Content: "done"}}
	out := DialogueProjection(history)
	if len(out) != 2 || out[0].Role != llm.RoleUser || out[1].Role != llm.RoleAssistant || len(out[1].ToolCalls) > 0 || out[1].ToolCallID != "" {
		t.Fatal("invalid chat context projection")
	}
}
func TestApplicationPreview(t *testing.T) {
	dir := os.Getenv("TARS_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview opt-in")
	}
	os.MkdirAll(dir, 0700)
	m := settingsModel(t)
	profile := m.chat.config.Profile
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "chat", Voice: "off", ProfilePath: "profiles/chat-veyra-local.json", Profile: &profile}}
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.openApplication()
	if err := os.WriteFile(filepath.Join(dir, "application-mode.txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestAgentSharedModelSettingsCarryHistoryAndDraft(t *testing.T) {
	m := settingsModel(t)
	profile := m.chat.config.Profile
	audio := m.chat.config
	history := m.chat.current.History()
	m.chat = nil
	m.audioConfig = audio
	m.seedHistory = history
	m.stateFile = ""
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "agent", Voice: "off", Workspace: t.TempDir(), Profile: &profile}}
	m.editSettings("model")
	if m.dialog == nil || m.dialog.form.Workspace.Section != "model" {
		t.Fatal("Agent model editor unavailable")
	}
	focusSetting(m, "model")
	m.dialog.form.Editor.SetValue("another-model")
	m.applyForm()
	if m.modeChange == nil || m.modeChange.Preferences.Profile.Provider.Model != "another-model" || m.modeChange.Draft != "unfinished draft" || len(m.modeChange.History) != len(history) || m.state != stDone {
		t.Fatal("settings lost context or started a run")
	}
	if m.chat != nil {
		t.Fatal("Agent settings created a Chat runner")
	}
}
func TestSharedApplicationPageRetainsStagedModel(t *testing.T) {
	m := settingsModel(t)
	profile := m.chat.config.Profile
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "chat", Voice: "off", Workspace: t.TempDir(), Profile: &profile}}
	m.editSettings("model")
	focusSetting(m, "model")
	m.dialog.form.Editor.SetValue("selected-model")
	m.editSettings("application")
	m.dialog.form.Editor.SetValue("agent")
	m.applyForm()
	if m.modeChange == nil || m.modeChange.Preferences.Profile.Provider.Model != "selected-model" {
		t.Fatal("shared settings lost staged values")
	}
	if m.modeChange.Preferences.Mode != "agent" {
		t.Fatalf("mode = %q", m.modeChange.Preferences.Mode)
	}
	// A mode change must not adopt the Chat directory. The Agent writes
	// state.json and its mode beside conversation.json, which flips that
	// directory's settings.json to "agent": the transcript stops being
	// offered by the session list and the next delete removes it.
	if m.modeChange.Directory != "" {
		t.Fatalf("mode change carried a Chat directory into an Agent launch: %q", m.modeChange.Directory)
	}
}

// A change that does not switch mode stays in the session it was made in.
func TestSameModeSettingsChangeKeepsTheSessionDirectory(t *testing.T) {
	m := agentSettingsModel(t)
	// The Agent side reads its checkpoint before anything is written; a
	// missing one would fail for a reason unrelated to this check.
	m.stateFile = writeSessionState(t, t.TempDir(), "agent", []llm.Message{{Role: llm.RoleUser, Content: "task"}})
	original := m.sessionDirectory()
	m.application.Preferences.Workspace = original
	m.editSettings("application")
	focusSetting(m, "app-workspace")
	m.dialog.form.Editor.SetValue(t.TempDir())
	focusSetting(m, "app-voice")
	m.applyForm()
	if m.dialog != nil {
		t.Fatalf("apply refused: %q", m.dialog.form.Error)
	}
	if m.modeChange == nil {
		t.Fatal("no mode change queued")
	}
	if m.modeChange.Directory != original {
		t.Fatalf("same-mode change moved the session: %q -> %q", original, m.modeChange.Directory)
	}
}

// An Agent task must never be rooted in a directory that holds a Chat
// archive, whatever put it there.
// The Chat-archive guard disappears with the feature, so this now pins
// the rule that survives it: a task with no state file gets a directory
// of its own under the session root, and a task that already has one
// keeps it, so resuming does not relocate a session.
//
// The guard used to interrupt that second case, because a Chat archive
// kept a different state file in the same directory and the task's save
// rewrote the archive's settings.json. With every session in agent
// format there is no second file to collide with.
func TestAgentTaskKeepsTheDirectoryItAlreadyHas(t *testing.T) {
	m := sizeModel(t, testModel())
	root := t.TempDir()
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	m.ws = ws
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "agent", Workspace: root}}
	m.audioConfig = &ChatConfig{Directory: root, Profile: config.Profile{Version: 1}}

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

func TestApplicationSessionScanUsesSavedModeWithBothArchives(t *testing.T) {
	m := settingsModel(t)
	profile := m.chat.config.Profile
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "agent", Voice: "off", Workspace: t.TempDir(), Profile: &profile}}
	dir := m.sessionDirectory()
	m.stateFile = filepath.Join(dir, "state.json")
	if err := agent.SaveState(m.stateFile, []llm.Message{{Role: llm.RoleUser, Content: "question"}, {Role: llm.RoleAssistant, Content: "answer"}}); err != nil {
		t.Fatal(err)
	}
	if err := writeChatMode(m.stateFile, true); err != nil {
		t.Fatal(err)
	}
	if err := m.saveSessionSettings(); err != nil {
		t.Fatal(err)
	}
	entries := listSessionsIn(filepath.Dir(dir))
	if len(entries) != 1 || entries[0].mode != "Plan" {
		t.Fatalf("duplicated or wrong session mode: %+v", entries)
	}
	m.sessions = &sessionsState{entries: entries}
	_, handled := m.openApplicationSession(entries[0])
	if !handled || m.modeChange == nil || m.modeChange.Directory != dir || m.modeChange.Preferences.Mode != "agent" || len(m.modeChange.History) != 2 {
		t.Fatal("saved session failed to restore")
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
	profile := m.chat.config.Profile
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "chat", Voice: "off", Workspace: "D:\\Projects\\workspace", Profile: &profile}}
	for _, page := range []string{"model", "application", "profiles"} {
		m.editSettings(page)
		if err := os.WriteFile(filepath.Join(dir, "shared-"+page+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.closeDialog()
	m.header = headerState{global: true, focused: true, menu: "settings"}
	if err := os.WriteFile(filepath.Join(dir, "shared-chat-menu.txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := m.chat.config
	m.chat = nil
	m.audioConfig = cfg
	m.application.Preferences.Mode = "agent"
	if err := os.WriteFile(filepath.Join(dir, "shared-agent-menu.txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestAgentSeedReplacesLeadingSystemWithoutDroppingDialogue(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleSystem, Content: "old instructions"}, {Role: llm.RoleUser, Content: "hello"}, {Role: llm.RoleAssistant, Content: "hi"}}
	seed := agentSeed(history)
	if len(seed) != 3 || seed[0].Role != llm.RoleSystem || seed[0].Content != "" || seed[1].Content != "hello" || history[0].Content != "old instructions" {
		t.Fatal("competing prompt or mutated archive")
	}
}
func TestUnchangedSettingsDoNotRestart(t *testing.T) {
	m := settingsModel(t)
	profile := m.chat.config.Profile
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "chat", Voice: "off", Profile: &profile}}
	m.editSettings("model")
	m.applyForm()
	if m.modeChange != nil || m.dialog == nil {
		t.Fatal("unchanged Apply restarted or dismissed settings")
	}
}
func TestCommanderColorPreview(t *testing.T) {
	dir := os.Getenv("TARS_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview")
	}
	m := settingsModel(t)
	profile := m.chat.config.Profile
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "chat", Voice: "off", Profile: &profile}}
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 28})
	m.transcript.blocks = []block{userBlock("Hello there"), answerBlock("Hello. What would you like to work on?")}
	m.refreshContent()
	for _, name := range []string{"conversation", "sessions", "menu"} {
		m.navFocused = name == "sessions"
		if name == "menu" {
			m.header = headerState{global: true, focused: true, index: 4, menu: "settings", row: 0}
		}
		if err := os.WriteFile(filepath.Join(dir, "commander-"+name+".ansi"), []byte(m.View().Content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
