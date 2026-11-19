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

// DetailCommand keeps profile decoding outside the shared filesystem picker.
func (f *Model) DetailCommand() tea.Cmd {
	p := f.Picker
	if p == nil || p.Field != "load-profile" {
		return f.Model.DetailCommand()
	}
	if p.Loading || len(p.Filtered) == 0 {
		return nil
	}
	path := p.Filtered[p.Cursor]
	if p.DetailPath == path {
		return nil
	}
	p.DetailPath, p.Detail = path, ""
	id := p.ID
	return func() tea.Msg {
		return fieldform.DetailMsg{ID: id, Path: path, Text: CatalogDetail("load-profile", path)}
	}
}
