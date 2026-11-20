package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/screen"
)

type settingsDiscovery struct {
	dialog screen.DialogID
	picker fieldform.RequestID
	cancel context.CancelFunc
}

func (m *Model) cancelSettingsDiscovery() {
	if m.settingsDiscovery != nil {
		m.settingsDiscovery.cancel()
		m.settingsDiscovery = nil
	}
}

func (m *Model) syncSettingsDiscovery() {
	job := m.settingsDiscovery
	if job == nil {
		return
	}
	snapshot, ok := m.chrome.FormSnapshot(job.dialog)
	if !ok || !snapshot.PickerOpen || snapshot.PickerID != job.picker {
		m.cancelSettingsDiscovery()
	}
}

// discoverSettings admits only the current picker request. The effect captures
// values and the launch context, never the mutable form or application model.
func (m *Model) discoverSettings(request fieldform.DiscoveryRequest) tea.Cmd {
	snapshot, ok := m.chrome.FormSnapshot(m.preferencesDialog)
	if !ok || !snapshot.PickerOpen || snapshot.PickerID != request.ID {
		return nil
	}

	if m.preferences == nil || m.preferencesDialog != m.chrome.DialogID() {
		return nil
	}
	if job := m.settingsDiscovery; job != nil && job.dialog == m.preferencesDialog && job.picker == request.ID {
		return nil
	}
	provider, providerErr := m.preferences.Provider(request)
	if providerErr != nil {
		return func() tea.Msg { return fieldform.ScanMsg{ID: request.ID, Err: providerErr} }
	}
	m.cancelSettingsDiscovery()
	ctx, stop := context.WithCancel(m.base)
	m.settingsDiscovery = &settingsDiscovery{dialog: m.preferencesDialog, picker: request.ID, cancel: stop}
	return func() tea.Msg {
		defer stop()
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
