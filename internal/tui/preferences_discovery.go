package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/tui/settings"
)

// discoverSettings admits only the current picker request. The effect captures
// values and the launch context, never the mutable form or application model.
func (m *Model) discoverSettings(request settings.DiscoveryRequest) tea.Cmd {
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil {
		return nil
	}
	picker := m.chrome.Dialog.Form.Picker
	if picker == nil || picker.ID != request.ID {
		return nil
	}

	ctx := m.base
	return func() tea.Msg {
		discovery, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		options := app.BackendOptions{
			Kind:      request.Provider.Kind,
			URL:       request.Provider.URL,
			Model:     request.Provider.Model,
			APIKeyEnv: request.Provider.APIKeyEnv,
		}

		var items []string
		var err error
		switch request.Catalog {
		case settings.ModelCatalog:
			items, err = app.DiscoverModels(discovery, options)
			if err != nil {
				err = fmt.Errorf("%w; Esc returns to manual entry", err)
			}
		case settings.ReasoningCatalog:
			reasoning, reasoningErr := app.DiscoverReasoning(discovery, options)
			err = reasoningErr
			if err == nil {
				items = append([]string{"default"}, reasoning.Efforts...)
			}
		default:
			err = fmt.Errorf("unknown settings discovery request")
		}
		return settings.ScanMsg{ID: request.ID, Items: items, Err: err}
	}
}
