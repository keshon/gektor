package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/speech"
	"os"
	"path/filepath"
	"testing"
	"time"
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
func TestAgentVoiceCanEnableWithoutChatRunner(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	cfg := speech.Config{}
	m.audioConfig = &ChatConfig{Directory: t.TempDir(), Profile: config.Profile{Speech: &cfg}, NewVoice: func(context.Context, string) (speech.Service, error) { return &voiceService{}, nil }}
	if err := m.enableVoice(); err != nil {
		t.Fatal(err)
	}
	if m.chat != nil || !m.voiceEnabled {
		t.Fatal("agent speech required chat runner")
	}
	m.voice.Close()
}
func TestDialogueProjectionDoesNotOfferHistoricalTools(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleSystem, Content: "old instructions"}, {Role: llm.RoleUser, Content: "inspect files"}, {Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "x", Name: "read_file"}}}, {Role: llm.RoleTool, Content: "file content", ToolCallID: "x"}, {Role: llm.RoleAssistant, Content: "done"}}
	out := DialogueProjection(history)
	if len(out) != 2 || out[0].Role != llm.RoleUser || out[1].Role != llm.RoleAssistant || len(out[1].ToolCalls) > 0 || out[1].ToolCallID != "" {
		t.Fatal("invalid chat context projection")
	}
}

func TestAgentSpokenAnswerUsesIndependentAudio(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	service := &voiceService{}
	m.audioConfig = &ChatConfig{Profile: config.Profile{Speech: &speech.Config{}}, NewVoice: func(context.Context, string) (speech.Service, error) { return service, nil }, Directory: t.TempDir()}
	if err := m.enableVoice(); err != nil {
		t.Fatal(err)
	}
	defer m.voice.Close()
	done := make(chan doneMsg, 1)
	m.send = func(msg tea.Msg) { done <- msg.(doneMsg) }
	m.startRun(func(context.Context) (string, error) { return "Hello. I checked the files.", nil })
	select {
	case result := <-done:
		if result.err != nil || result.answer == "" {
			t.Fatal(result)
		}
	case <-time.After(time.Second):
		t.Fatal("agent speech did not finish")
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

func TestAgentSpeechSettingsDoNotCreateChatOrChangeTools(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	profile := config.Profile{Version: 1, Provider: config.Provider{Kind: "llama", URL: "http://127.0.0.1:8080", Model: "local", Ownership: "external"}, Generation: config.Generation{MaxTokens: 100, ContextLimit: 1000}, Speech: &speech.Config{Microphone: "old", Recorder: "ffmpeg", PlayerBinary: "ffplay", MaxSeconds: 120, Listening: speech.Listening{Ownership: "external", URL: "http://127.0.0.1:19082"}, Speaking: speech.Speaking{Provider: "chatterbox", Variant: "nano", Binary: "python", Model: "models/nano"}}}
	m.audioConfig = &ChatConfig{Profile: profile, Configure: func(p config.Profile) (ChatConfig, error) { return ChatConfig{Profile: p}, nil }}
	m.editSettings("speech")
	m.dialog.form.Editor.SetValue("new microphone")
	m.applyForm()
	if m.dialog != nil || m.chat != nil || m.audioConfig.Profile.Speech.Microphone != "new microphone" {
		t.Fatal("agent speech settings used chat runner")
	}
}
func TestAgentSharedModelSettingsCarryHistoryAndDraft(t *testing.T) {
	m := settingsModel(t)
	profile := m.chat.config.Profile
	audio := m.chat.config
	history := m.chat.current.History()
	m.chat = nil
	m.audioConfig = &audio
	m.seedHistory = history
	m.stateFile = ""
	m.application = &Application{Preferences: config.Preferences{Version: 1, Mode: "agent", Voice: "off", Workspace: t.TempDir(), Profile: &profile}}
	m.editSettings("model")
	if m.dialog == nil || m.dialog.form.workspace.section != "model" {
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
	original := m.sessionDirectory()
	m.editSettings("model")
	focusSetting(m, "model")
	m.dialog.form.Editor.SetValue("selected-model")
	m.editSettings("application")
	m.dialog.form.Editor.SetValue("agent")
	m.applyForm()
	if m.modeChange == nil || m.modeChange.Preferences.Profile.Provider.Model != "selected-model" || m.modeChange.Directory != original || m.modeChange.Preferences.Mode != "agent" {
		t.Fatal("shared settings lost staged values or session identity")
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
	m.audioConfig = &cfg
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
func TestStopPlaybackPreservesGenerationAndNewSpeechOwner(t *testing.T) {
	m := settingsModel(t)
	generation, cancelGeneration := context.WithCancel(context.Background())
	defer cancelGeneration()
	audio, cancelAudio := context.WithCancel(generation)
	clearOld := m.registerSpeech(cancelAudio)
	m.stopPlayback()
	if audio.Err() == nil || generation.Err() != nil {
		t.Fatal("playback stop canceled generation or left audio running")
	}
	next, cancelNext := context.WithCancel(generation)
	clearNext := m.registerSpeech(cancelNext)
	defer clearNext()
	clearOld()
	if !m.hasPlayback() || next.Err() != nil {
		t.Fatal("late old output removed current speech owner")
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
