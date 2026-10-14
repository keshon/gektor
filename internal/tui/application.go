package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	"github.com/keshon/tars/internal/agent"
	"github.com/keshon/tars/internal/character"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Application struct {
	Preferences    config.Preferences
	PreferencePath string
	Temporary      bool
}
type ModeChange struct {
	Preferences config.Preferences
	History     []llm.Message
	Draft       string
	Directory   string
	KeepAct     bool
}

func (m *ModeChange) Error() string { return "conversation mode changed" }
func (m *model) audio() *ChatConfig {
	if m.chat != nil {
		return &m.chat.config
	}
	return m.audioConfig
}
func (m *model) openApplication() {
	if m.application != nil && m.state == stDone {
		m.editSettings("application")
	}
}
func (m *model) settingsApplicationChanged() bool {
	f := m.dialog.form
	fields, ok := f.workspace.cache["application"]
	if !ok {
		return false
	}
	p := m.application.Preferences
	for _, field := range fields {
		switch field.key {
		case "app-mode":
			if field.value != p.Mode {
				return true
			}
		case "app-workspace":
			if field.value != p.Workspace {
				return true
			}
		case "app-voice":
			if field.value != m.currentVoiceMode() {
				return true
			}
		}
	}
	return false
}
func (m *model) currentVoiceMode() string {
	if m.listening != nil {
		return "live"
	}
	if m.voiceEnabled {
		return "ptt"
	}
	return "off"
}
func (m *model) saveApplication() error {
	if m.application == nil || m.application.Temporary {
		return nil
	}
	p := m.application.Preferences
	p.Voice = m.currentVoiceMode()
	if m.audio() != nil {
		profile := cloneSettingsProfile(m.audio().Profile)
		p.Profile = &profile
	}
	if err := m.saveSessionSettings(); err != nil {
		return err
	}
	return config.SavePreferences(m.application.PreferencePath, p)
}

// DialogueProjection carries text context without exposing historical tool calls to chat.
func DialogueProjection(history []llm.Message) []llm.Message {
	out := []llm.Message{}
	var user, answer string
	flush := func() {
		if user != "" && answer != "" {
			out = append(out, llm.Message{Role: llm.RoleUser, Content: user}, llm.Message{Role: llm.RoleAssistant, Content: answer})
		}
		user, answer = "", ""
	}
	for _, m := range history {
		switch m.Role {
		case llm.RoleUser:
			flush()
			user = m.Content
		case llm.RoleAssistant:
			if m.Content != "" {
				if answer != "" {
					answer += "\n"
				}
				answer += m.Content
			}
		case llm.RoleTool:
			if user != "" {
				answer += "\n[Prior tool output] " + m.Content
			}
		}
	}
	flush()
	return out
}

func (m *model) editAgentSpeech() {
	audio := m.audio()
	if audio == nil || audio.Profile.Speech == nil || audio.Configure == nil {
		return
	}
	profile := cloneSettingsProfile(audio.Profile)
	cfg := profile.Speech
	m.openForm("Speech services", "Updates speech only; Agent history and permissions stay intact.", "Apply", []formField{{"microphone", "Microphone", cfg.Microphone, false}, {"variant", "Chatterbox model", cfg.Speaking.Variant, false}, {"speech-model", "Speech model directory", cfg.Speaking.Model, false}, {"seconds", "Maximum recording seconds", strconv.Itoa(cfg.MaxSeconds), false}}, func(values map[string]string) error {
		if err := applySettingsValues(&profile, "speech", values); err != nil {
			return err
		}
		next, err := audio.Configure(profile)
		if err != nil {
			return err
		}
		if m.voice != nil {
			m.voice.Close()
			m.voice = nil
		}
		m.voiceEnabled = false
		m.audioConfig = &next
		m.appendBlock(markerBlock("Speech settings applied; enable voice again"))
		return nil
	})
	m.dialog.form.workspace = &settingsWorkspace{section: "speech", cache: map[string][]formField{}, original: map[string][]formField{}}
}

// requestProfileChange replaces owned services without starting an Agent run.
func (m *model) requestProfileChange(profile config.Profile) error {
	if m.application == nil {
		return fmt.Errorf("application settings are unavailable for this launch")
	}
	if err := profile.Validate(); err != nil {
		return err
	}

	if profile.Conversation != nil && profile.Conversation.Character != "" {
		card, err := character.Load(profile.Conversation.Character)
		if err != nil {
			return err
		}
		if _, err := card.Greeting(profile.Conversation.GreetingIndex); err != nil {
			return err
		}
	}
	if profile.Provider.Ownership == "managed" {
		for _, path := range []string{profile.Provider.Runtime, profile.Provider.ModelFile} {
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("model configuration: %w", err)
			}
			if info.IsDir() {
				return fmt.Errorf("select a file: %s", path)
			}
		}
	}
	p := m.application.Preferences
	p.Profile = &profile
	p.Voice = m.currentVoiceMode()
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.workspace != nil && m.dialog.form.workspace.loadedPath != "" {
		p.ProfilePath = m.dialog.form.workspace.loadedPath
	}
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.workspace != nil {
		for _, field := range m.dialog.form.workspace.cache["application"] {
			switch field.key {
			case "app-mode":
				p.Mode = field.value
			case "app-workspace":
				p.Workspace = strings.TrimSpace(field.value)
			case "app-voice":
				p.Voice = field.value
			}
		}
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Mode == "agent" {
		absolute, err := filepath.Abs(p.Workspace)
		if err != nil {
			return err
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("select an existing Agent workspace")
		}
		p.Workspace = absolute
	}
	if p.Mode == "chat" && profile.Conversation == nil {
		return fmt.Errorf("Chat requires a profile with conversation settings")
	}
	if p.Voice != "off" && profile.Speech == nil {
		return fmt.Errorf("this profile has no speech configuration")
	}
	if err := m.saveCustomSettings(profile); err != nil {
		return err
	}
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.workspace != nil && m.dialog.form.workspace.loadedPath != "" {
		p.ProfilePath = m.dialog.form.workspace.loadedPath
	}
	history := append([]llm.Message(nil), m.seedHistory...)
	if m.chat != nil && m.chat.current != nil {
		history = m.chat.current.History()
	} else if m.stateFile != "" {
		var err error
		history, err = agent.LoadState(m.stateFile)
		if err != nil {
			return err
		}
	}
	m.modeChange = &ModeChange{Preferences: p, History: history, Draft: m.input.Value(), Directory: m.sessionDirectory(), KeepAct: m.chat == nil && p.Mode == "agent" && !m.plan}
	m.pendingCommands = append(m.pendingCommands, tea.Quit)
	return nil
}
