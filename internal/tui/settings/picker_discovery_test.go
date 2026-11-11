package settings

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/form"
)

func modelPicker(t *testing.T) *Model {
	t.Helper()
	fields := []form.Field{
		{Key: "kind", Value: "openai"},
		{Key: "endpoint", Value: "http://127.0.0.1:8080"},
		{Key: "key-env", Value: "TEST_API_KEY"},
		{Key: "model", Value: "original"},
	}
	model := NewModel(fields, textarea.New())
	model.Focus(3, model.FieldVisible)
	return &model
}

func TestModelDiscoveryCopiesBackendSelection(t *testing.T) {
	model := modelPicker(t)
	command := model.OpenPicker(Props{Width: 80, Height: 24})
	model.Fields[0].Value = "kobold"
	model.Fields[1].Value = "http://different-endpoint"

	request := command().(DiscoveryRequest)
	want := config.Provider{
		Kind:      "openai",
		URL:       "http://127.0.0.1:8080",
		APIKeyEnv: "TEST_API_KEY",
	}
	if request.Provider != want || request.Catalog != ModelCatalog || request.ID != model.Picker.ID {
		t.Fatalf("discovery retained mutable form data: %+v", request)
	}
	if !model.Picker.Loading {
		t.Fatal("discovery did not mark the picker as loading")
	}
}

func TestResultsCannotCrossPickerOrFormLifetimes(t *testing.T) {
	first := modelPicker(t)
	first.OpenPicker(Props{Width: 80, Height: 24})
	oldID := first.Picker.ID
	first.PickerKey(tea.KeyPressMsg{Code: tea.KeyEscape}, Props{})
	first.OpenPicker(Props{Width: 80, Height: 24})

	second := modelPicker(t)
	second.OpenPicker(Props{Width: 80, Height: 24})
	for _, current := range []*Model{first, second} {
		current.AcceptScan(ScanMsg{ID: oldID, Items: []string{"stale-model"}})
		if len(current.Picker.Items) != 0 || !current.Picker.Loading {
			t.Fatal("stale result replaced another picker")
		}
		current.AcceptScan(ScanMsg{ID: current.Picker.ID, Items: []string{"current-model"}})
		if len(current.Picker.Items) != 1 || current.Picker.Items[0] != "current-model" || current.Picker.Loading {
			t.Fatal("current discovery was rejected")
		}
	}
}

func TestReasoningDiscoveryUsesStagedModel(t *testing.T) {
	profile := profile()
	profile.Provider.Kind = "chatgpt"
	profile.Provider.Model = "original"
	draft := New(profile)
	fields := []form.Field{{Key: "kind", Value: "chatgpt"}, {Key: "model", Value: "staged"}}
	draft.Visit("model", fields)
	draft.Store("model", fields)
	model := NewModel([]form.Field{{Key: "reasoning-effort", Value: "default"}}, textarea.New())
	model.EditSession = &EditSession{Draft: draft, Section: "generation"}
	model.Focus(0, model.FieldVisible)

	command := model.OpenPicker(Props{Width: 80, Height: 24})
	request := command().(DiscoveryRequest)
	if request.Catalog != ReasoningCatalog || request.Provider.Model != "staged" || request.Provider.Kind != "chatgpt" {
		t.Fatalf("lost staged provider: %+v", request)
	}
}
