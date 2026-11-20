package settings

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
)

func TestInvalidProfileKeepsApplyDisabledAndPreservesStagedValues(t *testing.T) {
	p := profile()
	_, fields := PageFields(p, "profiles", config.Preferences{})
	m := newTestForm(fields, textarea.New())
	m.EditSession = &EditSession{Draft: New(p), Section: "profiles"}
	m.EditSession.Draft.Visit("profiles", fields)
	path := filepath.Join(t.TempDir(), "broken.json")
	err := os.WriteFile(path, []byte("invalid JSON"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Fields {
		if m.Fields[i].Key == "load-profile" {
			m.Fields[i].Value = path
		}
	}
	m.Prepare()
	if !m.DisablePrimary || m.FieldErrors["load-profile"] == "" || m.EditSession.LoadedPath != "" {
		t.Fatal("invalid profile was accepted or lacked field feedback")
	}
	if m.EditSession.Draft.Base().Provider != p.Provider {
		t.Fatal("invalid profile replaced staged configuration")
	}
}

func TestScreenEmitsApplicationIntentsWithoutChangingProfile(t *testing.T) {
	p := profile()
	_, fields := PageFields(p, "generation", config.Preferences{})
	m := newTestForm(fields, textarea.New())
	m.EditSession = &EditSession{Draft: New(p), Section: "generation"}
	m.EditSession.Draft.Visit("generation", fields)
	m.Focus(0, m.FieldVisible)
	m.Editor.SetValue("0.8")
	intent, _ := m.UpdateKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, fieldform.Props{})
	if intent.Action != fieldform.Apply || p.Generation.Temperature != 0 {
		t.Fatal("screen applied configuration instead of requesting Apply")
	}
	// One section per screen: there is no in-form category navigation left
	// to keep focus away from the fields, so F6 is ordinary input.
	m.UpdateKey(tea.KeyPressMsg{Code: tea.KeyF6}, fieldform.Props{})
	if m.InputOwner() != fieldform.FieldInput {
		t.Fatalf("F6 took the editor away from its field: %v", m.InputOwner())
	}
}
