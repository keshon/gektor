package tui

import (
	"testing"

	"github.com/keshon/gektor/internal/tui/settings"
)

func TestStaleDiscoveryRequestDoesNotStartBackendWork(t *testing.T) {
	m := settingsModel(t)
	m.editSettings("model")
	f := m.chrome.Dialog.Form
	for i, field := range f.Fields {
		if field.Key == "model" {
			f.Focus(i, f.FieldVisible)
			break
		}
	}
	command := f.OpenPicker(m.chrome.SettingsProps())
	request := command().(settings.DiscoveryRequest)
	m.chrome.Close()

	if m.discoverSettings(request) != nil {
		t.Fatal("closed picker started backend discovery")
	}
	m.editSettings("model")
	f = m.chrome.Dialog.Form
	for i, field := range f.Fields {
		if field.Key == "model" {
			f.Focus(i, f.FieldVisible)
			break
		}
	}
	f.OpenPicker(m.chrome.SettingsProps())
	if m.discoverSettings(request) != nil {
		t.Fatal("old request started discovery for a new form")
	}
}
