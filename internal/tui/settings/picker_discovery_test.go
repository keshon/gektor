package settings

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/components/form"
)

func modelPicker(t *testing.T) *testForm {
	t.Helper()
	fields := []form.Field{
		{Key: "kind", Value: "openai"},
		{Key: "endpoint", Value: "http://127.0.0.1:8080"},
		{Key: "key-env", Value: "TEST_API_KEY"},
		{Key: "model", Value: "original"},
	}
	model := newTestForm(fields, textarea.New())
	model.Focus(3, model.FieldVisible)
	return &model
}

func TestModelDiscoveryCopiesBackendSelection(t *testing.T) {
	model := modelPicker(t)
	command := model.OpenPicker(fieldform.Props{Width: 80, Height: 24})
	model.Fields[0].Value = "kobold"
	model.Fields[1].Value = "http://different-endpoint"

	request := command().(fieldform.DiscoveryRequest)
	want := config.Provider{
		Kind:      "openai",
		URL:       "http://127.0.0.1:8080",
		APIKeyEnv: "TEST_API_KEY",
	}
	provider, err := model.Provider(request)
	if err != nil || provider != want || request.Source != "model" || request.ID != model.Picker.ID {
		t.Fatalf("discovery retained mutable form data: %+v", request)
	}
	if !model.Picker.Loading {
		t.Fatal("discovery did not mark the picker as loading")
	}
}

func TestResultsCannotCrossPickerOrFormLifetimes(t *testing.T) {
	first := modelPicker(t)
	first.OpenPicker(fieldform.Props{Width: 80, Height: 24})
	oldID := first.Picker.ID
	first.PickerKey(tea.KeyPressMsg{Code: tea.KeyEscape}, fieldform.Props{})
	first.OpenPicker(fieldform.Props{Width: 80, Height: 24})

	second := modelPicker(t)
	second.OpenPicker(fieldform.Props{Width: 80, Height: 24})
	for _, current := range []*testForm{first, second} {
		current.AcceptScan(fieldform.ScanMsg{ID: oldID, Items: []string{"stale-model"}})
		if len(current.Picker.Items) != 0 || !current.Picker.Loading {
			t.Fatal("stale result replaced another picker")
		}
		current.AcceptScan(fieldform.ScanMsg{ID: current.Picker.ID, Items: []string{"current-model"}})
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
	model := newTestForm([]form.Field{{Key: "reasoning-effort", Value: "default"}}, textarea.New())
	model.EditSession = &EditSession{Draft: draft, Section: "generation"}
	model.Focus(0, model.FieldVisible)

	command := model.OpenPicker(fieldform.Props{Width: 80, Height: 24})
	request := command().(fieldform.DiscoveryRequest)
	provider, err := model.Provider(request)
	if err != nil || request.Source != "reasoning" || provider.Model != "staged" || provider.Kind != "chatgpt" {
		t.Fatalf("lost staged provider: %+v", request)
	}
}

func TestSwitchingProviderPresetUsesNewConnectionInModelPicker(t *testing.T) {
	model := modelPicker(t)
	model.Fields[0].Value = "openrouter"
	model.Prepare()
	model.Focus(3, model.FieldVisible)

	command := model.OpenPicker(fieldform.Props{Width: 80, Height: 24})
	request := command().(fieldform.DiscoveryRequest)
	provider, err := model.Provider(request)
	want := config.Provider{Kind: "openrouter", URL: "https://openrouter.ai/api/v1"}
	if err != nil || provider != want {
		t.Fatal(provider, err)
	}
	if model.Fields[3].Value != "" {
		t.Fatal("model from the old endpoint survived the switch")
	}
}
