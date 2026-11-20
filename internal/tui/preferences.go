package tui

import (
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
)

func (m *Model) editSettings(section string) {
	if !m.agent.Status().Idle || (m.workflows != nil && m.workflows.State().Busy) {
		return
	}
	currentSettings := m.profile
	if currentSettings == nil {
		return
	}
	var ws *settings.EditSession
	if m.preferences != nil && m.preferencesDialog == m.chrome.DialogID() && m.chrome.Dialog != nil && m.chrome.Dialog.Form != nil {
		ws = m.preferences.EditSession
		if ws != nil {
			m.preferences.Prepare()
			m.preferences.StorePage()
		}
	}
	if ws == nil {
		ws = &settings.EditSession{Draft: settings.New(currentSettings.Profile), PersistCurrent: m.application != nil && m.application.Temporary}
	}
	ws.Section = section
	ws.AccountLabel = m.launch.AccountLabel
	profile := ws.Draft.Base()
	if section != "model" {
		if modelFields, ok := ws.Draft.Fields("model"); ok {
			for _, field := range modelFields {
				if field.Key == "kind" {
					profile.Provider.Kind = field.Value
				}
			}
		}
	}
	preferences := config.Preferences{}
	if m.application != nil {
		preferences = m.application.Preferences
	}
	title, fields := settings.PageFields(profile, section, preferences)

	if m.chrome.Dialog != nil {
		m.chrome.Close()
	}
	m.openForm(screen.Form{
		Title:      title,
		Hint:       settings.Hint(section),
		ApplyLabel: "Apply",
		Fields:     fields,
	}, func(_ map[string]string) error {
		m.preferences.StorePage()
		profile, err := ws.Draft.Resolve()
		if err != nil {
			return err
		}

		// Applying settings restarts the launch so the new profile is composed
		// into fresh services. Nothing is applied in place: the session that
		// reads the old one must not keep running against the new.
		return m.requestProfileChange(profile)
	})
	m.chrome.Dialog.Form.SaveCopy = true
	m.preferences = &settings.Model{Model: &m.chrome.Dialog.Form.Model, EditSession: ws}
	m.preferencesDialog = m.chrome.DialogID()
	m.preferences.Prepare()
	saved := ws.Draft.Visit(section, fields)
	m.chrome.Dialog.Form.Fields = saved
	m.chrome.Dialog.Form.Index = len(saved)
	m.chrome.FocusForm(0)
	m.chrome.Dialog.Title = "Settings"
	m.chrome.Dialog.Form.Title = "Settings / " + title
}
