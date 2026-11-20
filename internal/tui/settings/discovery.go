package settings

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
)

// Provider resolves a copied picker request within this settings edit session.
func (f *Model) Provider(request fieldform.DiscoveryRequest) (config.Provider, error) {
	if request.Source == "model" {
		return config.Provider{
			Kind:      request.Values["kind"],
			URL:       request.Values["endpoint"],
			APIKeyEnv: request.Values["key-env"],
		}, nil
	}
	if f.EditSession == nil {
		return config.Provider{}, fmt.Errorf("reasoning options unavailable")
	}

	profile := f.EditSession.Draft.Base()
	if initial, ok := f.EditSession.Draft.Fields("model"); ok {
		values := make(map[string]string, len(initial))
		for _, field := range initial {
			values[field.Key] = field.Value
		}
		err := config.ApplySection(&profile, "model", values)
		if err != nil {
			return config.Provider{}, err
		}
	}
	return profile.Provider, nil
}

// ProfileDetail decodes a copied profile selection outside the update loop.
func ProfileDetail(request fieldform.DetailRequest) tea.Cmd {
	return func() tea.Msg {
		return fieldform.DetailMsg{ID: request.ID, Path: request.Path, Text: CatalogDetail("load-profile", request.Path)}
	}
}
