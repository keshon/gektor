package tui

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
)

func settingsPickerRequest(t *testing.T, m *Model) fieldform.DiscoveryRequest {
	t.Helper()
	m.editSettings("model")
	f := formSnapshot(m)
	for i, field := range f.Fields {
		if field.Key == "model" {
			m.focusForm(i)
			return m.chrome.OpenFormPicker(m.chrome.DialogID())().(fieldform.DiscoveryRequest)
		}
	}
	t.Fatal("model picker missing")
	return fieldform.DiscoveryRequest{}
}

func TestClosingPickerCancelsAdmittedDiscoveryBeforeCommandRuns(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	m := settingsModel(t)
	request := settingsPickerRequest(t, m)
	request.Values["kind"], request.Values["endpoint"] = "openai", server.URL+"/v1"
	command := m.discoverSettings(request)
	if command == nil || m.discoverSettings(request) != nil {
		t.Fatal("request not admitted exactly once")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	result := command().(fieldform.ScanMsg)
	if result.Err == nil || requests.Load() != 0 {
		t.Fatal("closed picker performed an admitted but unexecuted discovery")
	}
	m.acceptSettings(result)
	if snapshot, ok := m.chrome.FormSnapshot(m.preferencesDialog); !ok || snapshot.PickerOpen {
		t.Fatal("late cancellation result reopened the picker")
	}
}

func TestClosingSettingsCancelsActiveDiscovery(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	m := settingsModel(t)
	request := settingsPickerRequest(t, m)
	request.Values["kind"], request.Values["endpoint"] = "openai", server.URL+"/v1"
	command := m.discoverSettings(request)
	done := make(chan tea.Msg, 1)
	go func() { done <- command() }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		m.cancelSettingsDiscovery()
		t.Fatal("discovery did not start")
	}
	m.chrome.Open("replacement", []string{"keep"})
	m.settleControls()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		m.cancelSettingsDiscovery()
		t.Fatal("replacement did not cancel the discovery request")
	}
	select {
	case result := <-done:
		m.acceptSettings(result)
	case <-time.After(2 * time.Second):
		t.Fatal("canceled discovery did not finish")
	}
	if dialogSnapshot(m).Title != "replacement" {
		t.Fatal("late discovery changed replacement dialog")
	}
}

func TestStaleDiscoveryRequestDoesNotStartBackendWork(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("model")
	f := formSnapshot(m)
	for i, field := range f.Fields {
		if field.Key == "model" {
			m.focusForm(i)
			break
		}
	}
	command := m.chrome.OpenFormPicker(m.chrome.DialogID())
	request := command().(fieldform.DiscoveryRequest)
	m.chrome.Close()

	if m.discoverSettings(request) != nil {
		t.Fatal("closed picker started backend discovery")
	}
	m.editSettings("model")
	f = formSnapshot(m)
	for i, field := range f.Fields {
		if field.Key == "model" {
			m.focusForm(i)
			break
		}
	}
	m.chrome.OpenFormPicker(m.chrome.DialogID())
	if m.discoverSettings(request) != nil {
		t.Fatal("old request started discovery for a new form")
	}
}
