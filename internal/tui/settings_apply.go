package tui

import (
	"path/filepath"

	"github.com/keshon/tars/internal/app"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/speech"
)

// speechSettingsChanged compares a staged speech configuration against the
// live one, ignoring Enabled.
//
// Enabled is runtime state, not a setting: enabling voice, disabling it,
// and previewing a sample all flip it on the live profile. A settings
// draft is a snapshot taken when the form opened, so any of those inside
// an open dialog read as "the user changed their speech settings" and
// switched voice off on Apply — silently, with the transcript reporting
// only that settings were applied.
func speechSettingsChanged(staged, live *speech.Config) bool {
	if staged == nil || live == nil {
		return staged != live
	}
	a, b := *staged, *live
	a.Enabled, b.Enabled = false, false
	return a != b
}

// appliedSections are the staged sections Resolve actually writes.
// Profiles and Application are not among them: the first is saved with
// Ctrl+D, the second applied from its own page. Their buttons still read
// as live because the draft is dirty, so applying has to say plainly that
// nothing was applied rather than claim a success that did not happen.
func appliedSections(ws *settingsWorkspace) []string {
	var out []string
	for _, section := range ws.Draft.ChangedSections() {
		if section != "profiles" && section != "application" {
			out = append(out, section)
		}
	}
	return out
}

// applyChatSettings prepares and persists the candidate before transferring
// service ownership. Every error leaves the active runtime and draft intact.
func (m *model) applyChatSettings(profile config.Profile, ws *settingsWorkspace) error {
	// A loaded setup has no diff to report but must still be committed:
	// its base is the profile, not the session's.
	if len(appliedSections(ws)) == 0 && !ws.Draft.Loaded() {
		m.appendBlock(markerBlock("Nothing to apply. Profiles saves a copy with Ctrl+D; Application changes apply from that page."))
		return nil
	}
	speechChanged := speechSettingsChanged(profile.Speech, m.chat.config.Profile.Speech)
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
