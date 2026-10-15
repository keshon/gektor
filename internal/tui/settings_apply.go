package tui

import (
	"os"
	"path/filepath"
	"reflect"

	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/filetxn"
	"github.com/keshon/tars/internal/llm"
)

func closeSettingsClient(client llm.Client) {
	if c, ok := client.(interface{ Close() }); ok {
		c.Close()
	}
}

// applyChatSettings prepares and persists the candidate before transferring
// service ownership. Every error leaves the active runtime and draft intact.
func (m *model) applyChatSettings(profile config.Profile, ws *settingsWorkspace) error {
	next, err := m.chat.config.Configure(profile)
	if err != nil {
		return err
	}
	reuse := profile.Provider == m.chat.config.Profile.Provider && profile.Generation.ContextLimit == m.chat.config.Profile.Generation.ContextLimit
	if reuse {
		closeSettingsClient(next.Client)
		next.Client = m.chat.config.Client
	}
	installed := false
	defer func() {
		if !installed && !reuse {
			closeSettingsClient(next.Client)
		}
	}()
	var updated *chat.Conversation
	if m.chat.current != nil {
		updated, err = m.chat.current.Reconfigured(next.Client, next.Settings)
		if err != nil {
			return err
		}
	}
	speechChanged := !reflect.DeepEqual(profile.Speech, m.chat.config.Profile.Speech)
	custom, err := m.customSettingsPath()
	if err != nil {
		return err
	}
	var writes []filetxn.Write
	if custom != "" {
		writes = append(writes, filetxn.Write{Path: custom, Save: func(path string) error { return config.SaveCopy(profile, path) }})
	}
	var preferences config.Preferences
	if m.application != nil {
		preferences = m.application.Preferences
		p := profile.Clone()
		preferences.Profile = &p
		preferences.Voice = m.currentVoiceMode()
		if speechChanged {
			preferences.Voice = "off"
		}
		if ws.loadedPath != "" {
			preferences.ProfilePath = ws.loadedPath
		}
		if custom != "" {
			preferences.ProfilePath = custom
		}
		save := func(path string) error { return config.SavePreferences(path, preferences) }
		if m.stateFile != "" {
			writes = append(writes, filetxn.Write{Path: filepath.Join(m.sessionDirectory(), "settings.json"), Save: save})
		}
		if custom != "" && !m.application.Temporary {
			writes = append(writes, filetxn.Write{Path: m.application.PreferencePath, Save: save})
		}
	}
	if updated != nil && m.stateFile != "" {
		if _, err := os.Stat(m.stateFile); err == nil {
			writes = append(writes, filetxn.Write{Path: m.stateFile, Save: updated.Save})
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if err := filetxn.Replace(writes); err != nil {
		return err
	}

	// From here onward there are no fallible operations: ownership can change.
	if speechChanged {
		m.stopListening()
		if m.voice != nil {
			m.voice.Close()
			m.voice = nil
		}
		m.voiceEnabled = false
	}
	if !reuse {
		closeSettingsClient(m.chat.config.Client)
	}
	m.chat.current, m.chat.config = updated, next
	installed = true
	if m.application != nil {
		m.application.Preferences = preferences
	}
	if custom != "" {
		ws.loadedPath, ws.autoSaved = custom, true
	}
	m.backendKind = profile.Provider.Kind
	m.backendURL = safeEndpoint(profile.Provider.URL)
	m.modelName = profile.Provider.Model
	m.limit, m.responseCap, m.streaming = next.Settings.ContextLimit, next.Settings.MaxTokens, next.Stream
	message := "Settings applied to this session"
	if ws.autoSaved {
		message += " · saved " + filepath.Base(ws.loadedPath)
	}
	m.appendBlock(markerBlock(message))
	for _, warning := range next.Warnings {
		m.appendBlock(markerBlock("Character warning: " + warning))
	}
	return nil
}
