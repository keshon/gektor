package settings

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/config"
)

func TestScreenEmitsApplicationIntentsWithoutChangingProfile(t *testing.T) {
	p := profile()
	_, fields := PageFields(p, "generation", config.Preferences{})
	m := NewModel(fields, textarea.New())
	m.Workspace = &Workspace{Draft: New(p), Section: "generation"}
	m.Workspace.Draft.Visit("generation", fields)
	m.Focus(0, m.FieldVisible)
	m.Editor.SetValue("0.8")
	intent, _ := m.UpdateKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, Props{})
	if intent.Action != Apply || p.Generation.Temperature != 0 {
		t.Fatal("screen applied configuration instead of requesting Apply")
	}
	m.UpdateKey(tea.KeyPressMsg{Code: tea.KeyF6}, Props{})
	intent, _ = m.UpdateKey(tea.KeyPressMsg{Code: tea.KeyDown}, Props{})
	if intent.Action != SwitchCategory || intent.Section != "profiles" || !m.Workspace.Nav {
		t.Fatal("category navigation lost its intent or focus", intent)
	}
}
