package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/internal/tui/settings"
)

func (m *Model) editSettings(section string) {
	if !m.agent.Status().Idle {
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
	m.chrome.OpenForm(title, "GEKTOR restarts to apply these. The conversation and draft are preserved.", "Apply", fields, func(_ map[string]string) error {
		m.chrome.Dialog.Form.StorePage()
		profile, err := ws.Draft.Resolve()
		if err != nil {
			return err
		}

		// Applying settings restarts the launch so the new profile is composed
		// into fresh services. Nothing is applied in place: the session that
		// reads the old one must not keep running against the new.
		return m.requestProfileChange(profile)
	})
	m.chrome.Dialog.Form.SaveCopy = m.saveSettingsCopy
	m.chrome.Dialog.Form.EditSession = ws
	m.chrome.Dialog.Form.DiscoverModels = remoteModelChoices
	m.chrome.Dialog.Form.DiscoverReasoning = remoteReasoningChoices
	saved := ws.Draft.Visit(section, fields)
	m.chrome.Dialog.Form.Fields = saved
	m.chrome.Dialog.Form.Index = len(saved)
	m.chrome.FocusForm(0)
	m.chrome.Dialog.Title = "Settings"
}

func remoteModelChoices(kind, endpoint, keyEnv string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	items, err := app.DiscoverModels(ctx, app.BackendOptions{Kind: kind, URL: endpoint, APIKeyEnv: keyEnv})
	if err != nil {
		return nil, fmt.Errorf("%w; Esc returns to manual entry", err)
	}
	return items, nil
}

func remoteReasoningChoices(kind, endpoint, model string) (providers.ReasoningOptions, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return app.DiscoverReasoning(ctx, app.BackendOptions{Kind: kind, URL: endpoint, Model: model})
}
