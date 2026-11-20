package fields

import tea "charm.land/bubbletea/v2"

// RequestID identifies a scan across picker lifetimes; compare only for equality.
type RequestID struct{ token *requestToken }
type requestToken struct{ value byte }

func newRequestID() RequestID { return RequestID{token: &requestToken{}} }

// DiscoveryRequest asks the application for one explicitly configured catalog.
type DiscoveryRequest struct {
	ID            RequestID
	Field, Source string
	Values        map[string]string
}
type ScanMsg struct {
	ID    RequestID
	Items []string
	Err   error
}

func (f *Model) discoverPicker(field string) tea.Cmd {
	p := f.Picker
	p.Loading = true
	request := DiscoveryRequest{ID: p.ID, Field: field, Source: p.Source, Values: f.Values()}
	return func() tea.Msg { return request }
}
