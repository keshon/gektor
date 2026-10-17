package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/character"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/session"
	"github.com/keshon/tars/internal/tui/settings"
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
		return m.chat.config
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
	fields, ok := f.Workspace.Draft.Fields("application")
	if !ok {
		return false
	}
	p := m.application.Preferences
	for _, field := range fields {
		switch field.Key {
		case "app-mode":
			if field.Value != p.Mode {
				return true
			}
		case "app-workspace":
			if field.Value != p.Workspace {
				return true
			}
		case "app-voice":
			if field.Value != m.currentVoiceMode() {
				return true
			}
		}
	}
	return false
}
// liveVoiceMode is the voice input mode of the running session.
func (m *model) liveVoiceMode() string {
	if m.listening != nil {
		return "live"
	}
	if m.voiceEnabled {
		return "ptt"
	}
	return "off"
}

// currentVoiceMode is the mode settings should persist.
//
// It prefers the mode recorded when the settings form opened rather than
// the live one. editSettings stops capture before showing the form, so
// reading the live state at apply time always answered push-to-talk: a
// reader in hands-free who opened any settings page and pressed Apply had
// their voice input downgraded for the rest of the session and every
// launch after it. The same skew made the Application page report itself
// as changed the moment it was opened, because its Voice field was built
// from the live mode and compared against the torn-down one.
func (m *model) currentVoiceMode() string {
	if ws := m.settingsWorkspace(); ws != nil && ws.VoiceMode != "" {
		return ws.VoiceMode
	}
	return m.liveVoiceMode()
}

func (m *model) settingsWorkspace() *settingsWorkspace {
	if m.dialog == nil || m.dialog.form == nil {
		return nil
	}
	return m.dialog.form.Workspace
}
func (m *model) saveApplication() error {
	if m.application == nil || m.application.Temporary {
		return nil
	}
	p := m.application.Preferences
	p.Voice = m.currentVoiceMode()
	if m.audio() != nil {
		profile := m.audio().Profile.Clone()
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

// editAgentSpeech edits speech for an Agent that has no Application to
// persist through. voice is the mode the reader had on arrival, because
// editSettings has already stopped capture by the time this runs.
func (m *model) editAgentSpeech(voice string) {
	audio := m.audio()
	if audio == nil || audio.Profile.Speech == nil || audio.Configure == nil {
		return
	}
	profile := audio.Profile.Clone()
	cfg := profile.Speech
	m.openForm("Speech services", "Updates speech only; Agent history and permissions stay intact.", "Apply", []formField{{Key: "microphone", Label: "Microphone", Value: cfg.Microphone, Multiline: false}, {Key: "variant", Label: "Chatterbox model", Value: cfg.Speaking.Variant, Multiline: false}, {Key: "speech-model", Label: "Speech model directory", Value: cfg.Speaking.Model, Multiline: false}, {Key: "seconds", Label: "Maximum recording seconds", Value: strconv.Itoa(cfg.MaxSeconds), Multiline: false}}, func(values map[string]string) error {
		if err := config.ApplySection(&profile, "speech", values); err != nil {
			return err
		}
		if _, err := m.runtime().Apply(profile, nil, nil); err != nil {
			return err
		}
		m.voiceEnabled = false

		m.appendBlock(markerBlock("Speech settings applied; enable voice again"))
		return nil
	})
	m.dialog.form.Workspace = &settingsWorkspace{Section: "speech", Draft: settings.New(audio.Profile), VoiceMode: voice}
	m.dialog.form.Workspace.Draft.Visit("speech", m.dialog.form.Fields)
}

// requestProfileChange replaces owned services without starting an Agent run.
func (m *model) requestProfileChange(profile config.Profile) error {
	if m.application == nil {
		return fmt.Errorf("application settings are unavailable for this launch")
	}
	if err := profile.Validate(); err != nil {
		return err
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
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Workspace != nil && m.dialog.form.Workspace.LoadedPath != "" {
		p.ProfilePath = m.dialog.form.Workspace.LoadedPath
	}
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Workspace != nil {
		fields, _ := m.dialog.form.Workspace.Draft.Fields("application")
		for _, field := range fields {
			switch field.Key {
			case "app-mode":
				p.Mode = field.Value
			case "app-workspace":
				p.Workspace = strings.TrimSpace(field.Value)
			case "app-voice":
				p.Voice = field.Value
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
		// Materialize rather than refuse. Refusing left the reader on a
		// screen with no way to create one: the only sections that build a
		// Conversation are Character, You and Prompts, three pages away,
		// and nothing on the form named the requirement. The bundled Agent
		// profile carries no conversation block at all, so this was the
		// ordinary path rather than an edge case.
		profile.Conversation = &config.Conversation{
			UserName:     config.DefaultUserName,
			SystemPrompt: config.DefaultSystemPrompt,
		}
	}
	p.Profile = &profile
	if p.Mode == "chat" && profile.Conversation != nil && profile.Conversation.Character != "" {
		// Only Chat consumes a character, so only Chat can fail on one.
		// Validating it here for an Agent refused a mode switch because a
		// card the Agent will never read was missing from disk.
		card, err := character.Load(profile.Conversation.Character)
		if err != nil {
			return err
		}
		if _, err := card.Greeting(profile.Conversation.GreetingIndex); err != nil {
			return err
		}
	}
	if p.Voice != "off" && profile.Speech == nil {
		return fmt.Errorf("this profile has no speech configuration")
	}
	// Read the history before writing anything. A read failure used to leave
	// a profiles/custom file on disk holding settings that were never
	// applied and that nothing referenced, and left the workspace pointing
	// at it as though the apply had landed.
	history := append([]llm.Message(nil), m.seedHistory...)
	if m.chat != nil && m.chat.current != nil {
		history = m.chat.current.History()
	} else if m.stateFile != "" {
		loaded, err := session.ReadHistory(m.stateFile)
		if err != nil {
			return err
		}
		history = loaded
	}
	if err := m.saveCustomSettings(profile); err != nil {
		return err
	}
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Workspace != nil && m.dialog.form.Workspace.LoadedPath != "" {
		p.ProfilePath = m.dialog.form.Workspace.LoadedPath
	}
	// The session directory carries across a mode switch only when the mode
	// is unchanged. Handing a Chat directory to an Agent launch made the
	// Agent write state.json and its mode beside conversation.json, which
	// flipped settings.json to "agent": the chat transcript became
	// unreachable from the session list and the next delete removed it. A
	// mode change starts a session of its own, seeded with the projected
	// dialogue, and leaves the previous one openable.
	directory := ""
	if (p.Mode == "agent") == (m.chat == nil) {
		directory = m.sessionDirectory()
	}
	m.modeChange = &ModeChange{Preferences: p, History: history, Draft: m.input.Value(), Directory: directory, KeepAct: m.chat == nil && p.Mode == "agent" && !m.plan}
	m.pendingCommands = append(m.pendingCommands, tea.Quit)
	return nil
}
