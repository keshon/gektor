package tui

import (
	"testing"

	fieldform "github.com/keshon/gektor/internal/tui/components/fields"
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
	command := f.OpenPicker(m.chrome.FormProps())
	request := command().(fieldform.DiscoveryRequest)
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
	f.OpenPicker(m.chrome.FormProps())
	if m.discoverSettings(request) != nil {
		t.Fatal("old request started discovery for a new form")
	}
}
