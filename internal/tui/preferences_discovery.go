package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
)

// discoverSettings admits only the current picker request. The effect captures
// values and the launch context, never the mutable form or application model.
func (m *Model) discoverSettings(request fieldform.DiscoveryRequest) tea.Cmd {
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil {
		return nil
	}
	picker := m.chrome.Dialog.Form.Picker
	if picker == nil || picker.ID != request.ID {
		return nil
	}

	if m.preferences == nil || m.preferencesDialog != m.chrome.DialogID() {
		return nil
	}
	provider, providerErr := m.preferences.Provider(request)
	if providerErr != nil {
		return func() tea.Msg { return fieldform.ScanMsg{ID: request.ID, Err: providerErr} }
	}
	ctx := m.base
	return func() tea.Msg {
		discovery, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		options := app.BackendOptions{
			Kind:      provider.Kind,
			URL:       provider.URL,
			Model:     provider.Model,
			APIKeyEnv: provider.APIKeyEnv,
		}

		var items []string
		var err error
		switch request.Source {
		case "model":
			items, err = app.DiscoverModels(discovery, options)
			if err != nil {
				err = fmt.Errorf("%w; Esc returns to manual entry", err)
			}
		case "reasoning":
			reasoning, reasoningErr := app.DiscoverReasoning(discovery, options)
			err = reasoningErr
			if err == nil {
				items = append([]string{"default"}, reasoning.Efforts...)
			}
		default:
			err = fmt.Errorf("unknown settings discovery request")
		}
		return fieldform.ScanMsg{ID: request.ID, Items: items, Err: err}
	}
}
