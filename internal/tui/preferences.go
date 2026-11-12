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
	if m.chrome.Dialog != nil && m.chrome.Dialog.Form != nil {
		ws = m.chrome.Dialog.Form.EditSession
		if ws != nil {
			m.chrome.Dialog.Form.StorePage()
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
	}, editSettingsSubmission{
		ws: ws,
	})
	m.chrome.Dialog.Form.SaveCopy = true
	m.chrome.Dialog.Form.EditSession = ws
	saved := ws.Draft.Visit(section, fields)
	m.chrome.Dialog.Form.Fields = saved
	m.chrome.Dialog.Form.Index = len(saved)
	m.chrome.FocusForm(0)
	m.chrome.Dialog.Title = "Settings"
}

type editSettingsSubmission struct {
	ws *settings.EditSession
}

func (edit editSettingsSubmission) apply(m *Model, _ map[string]string) error {
	m.chrome.Dialog.Form.StorePage()
	profile, err := edit.ws.Draft.Resolve()
	if err != nil {
		return err
	}

	// Applying settings restarts the launch so the new profile is composed
	// into fresh services. Nothing is applied in place: the session that
	// reads the old one must not keep running against the new.
	return m.requestProfileChange(profile)
}
