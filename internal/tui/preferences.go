package tui

import (
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/pkg/tui/screen"
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
	if m.preferences != nil && m.preferencesDialog == m.chrome.DialogID() && m.chrome.DialogInfo().Kind == screen.FormDialog {
		ws = m.preferences.EditSession
		if ws != nil {
			m.preparePreferences()
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

	if m.chrome.DialogInfo().Kind != screen.NoDialog {
		m.chrome.Close()
	}
	saved := ws.Draft.Visit(section, fields)
	m.openForm(screen.Form{
		Title:       "Settings / " + title,
		DialogTitle: "Settings",
		SaveCopy:    true,
		Hint:        settings.Hint(section),
		ApplyLabel:  "Apply",
		Fields:      saved,
	}, func(_ map[string]string) error {
		m.preparePreferences()
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
	m.preferences = &settings.Model{EditSession: ws}
	m.preferencesDialog = m.chrome.DialogID()
	m.preparePreferences()
	m.chrome.FocusForm(0)
}

// preparePreferences supplies copied domain presentation to the controls-owned draft.
func (m *Model) preparePreferences() {
	if m.preferences == nil {
		return
	}
	snapshot, ok := m.chrome.FormSnapshot(m.preferencesDialog)
	if !ok {
		return
	}
	m.chrome.ConfigureForm(m.preferencesDialog, m.preferences.Prepare(snapshot))
}
