package settings

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
)

// RequestID identifies one scan across picker and form lifetimes. The zero
// value is invalid. Compare IDs only for equality; they contain no ordering.
type RequestID struct{ token *requestToken }

// Tokens have nonzero size so separate live requests have distinct addresses.
type requestToken struct{ value byte }

func newRequestID() RequestID { return RequestID{token: &requestToken{}} }

// DiscoveryKind selects the backend catalog requested by a picker.
type DiscoveryKind int

const (
	ModelCatalog DiscoveryKind = iota
	ReasoningCatalog
)

// DiscoveryRequest copies the selected backend values for the application to
// discover asynchronously. Settings never retains a backend or executes callbacks.
type DiscoveryRequest struct {
	ID       RequestID
	Catalog  DiscoveryKind
	Provider config.Provider
}

// ScanMsg returns a catalog or directory snapshot to exactly one picker scan.
type ScanMsg struct {
	ID    RequestID
	Items []string
	Err   error
}

func (f *Model) discoverPicker(field string) tea.Cmd {
	p := f.Picker
	p.Loading = true
	request := DiscoveryRequest{ID: p.ID, Catalog: ModelCatalog}
	if field == "model" {
		values := map[string]string{}
		for _, field := range f.Fields {
			values[field.Key] = field.Value
		}
		request.Provider = config.Provider{
			Kind:      values["kind"],
			URL:       values["endpoint"],
			APIKeyEnv: values["key-env"],
		}
		return request.command
	}

	request.Catalog = ReasoningCatalog
	if f.EditSession == nil {
		p.Loading = false
		p.Err = "reasoning options unavailable"
		return nil
	}
	profile := f.EditSession.Draft.Base()
	if fields, ok := f.EditSession.Draft.Fields("model"); ok {
		values := map[string]string{}
		for _, field := range fields {
			values[field.Key] = field.Value
		}
		err := config.ApplySection(&profile, "model", values)
		if err != nil {
			p.Loading = false
			p.Err = err.Error()
			return nil
		}
	}
	request.Provider = profile.Provider
	return request.command
}

func (r DiscoveryRequest) command() tea.Msg { return r }
