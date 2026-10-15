package tui

import (
	"path/filepath"
	"reflect"

	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
)

// applyChatSettings prepares and persists the candidate before transferring
// service ownership. Every error leaves the active runtime and draft intact.
func (m *model) applyChatSettings(profile config.Profile, ws *settingsWorkspace) error {
	speechChanged := !reflect.DeepEqual(profile.Speech, m.chat.config.Profile.Speech)
	custom, err := m.customSettingsPath()
	if err != nil {
		return err
	}
	files := app.SettingsFiles{Custom: custom, Archive: m.stateFile}
	var preferences config.Preferences
	if m.application != nil {
		preferences = m.application.Preferences
		p := profile.Clone()
		preferences.Profile = &p
		preferences.Voice = m.currentVoiceMode()
		if speechChanged {
			preferences.Voice = "off"
		}
		if ws.LoadedPath != "" {
			preferences.ProfilePath = ws.LoadedPath
		}
		if custom != "" {
			preferences.ProfilePath = custom
		}
		files.Preferences = preferences
		if m.stateFile != "" {
			files.Session = filepath.Join(m.sessionDirectory(), "settings.json")
		}
		if custom != "" && !m.application.Temporary {
			files.Application = m.application.PreferencePath
		}

	}
	updated, err := m.runtime().Apply(profile, m.chat.current, func(next app.Services, conversation *chat.Conversation) error {
		return files.Save(next.Profile, conversation)
	})
	if err != nil {
		return err
	}
	if speechChanged {
		m.stopListening()
		m.voiceEnabled = false
	}
	m.chat.current = updated
	next := m.chat.config

	if m.application != nil {
		m.application.Preferences = preferences
	}
	if custom != "" {
		ws.LoadedPath, ws.AutoSaved = custom, true
	}
	m.backendKind = profile.Provider.Kind
	m.backendURL = safeEndpoint(profile.Provider.URL)
	m.modelName = profile.Provider.Model
	m.limit, m.responseCap, m.streaming = next.Settings.ContextLimit, next.Settings.MaxTokens, next.Stream
	message := "Settings applied to this session"
	if ws.AutoSaved {
		message += " · saved " + filepath.Base(ws.LoadedPath)
	}
	m.appendBlock(markerBlock(message))
	for _, warning := range next.Warnings {
		m.appendBlock(markerBlock("Character warning: " + warning))
	}
	return nil
}
