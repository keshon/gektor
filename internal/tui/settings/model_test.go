package settings

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/config"
)

func TestScreenEmitsApplicationIntentsWithoutChangingProfile(t *testing.T) {
	p := profile()
	_, fields := PageFields(p, "generation", config.Preferences{}, "off")
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

func TestScreenViewDoesNotDiscoverChoicesOrMutateEditor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "card.json")
	if err := os.WriteFile(path, []byte(`{"name":"Ada","alternate_greetings":["Hi"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := profile()
	p.Conversation.Character = path
	_, fields := PageFields(p, "character", config.Preferences{}, "off")
	m := NewModel(fields, textarea.New())
	m.Workspace = &Workspace{Draft: New(p), Section: "character"}
	m.Workspace.Draft.Visit("character", fields)
	m.Focus(0, m.FieldVisible)
	choices := maps.Clone(m.Choices)
	before := slices.Clone(m.Fields)
	index := m.Index
	value := m.Editor.Value()
	// Changing the file cannot make View perform another discovery.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m.View(Props{Width: 100, Height: 30})
	m.View(Props{Width: 60, Height: 20})
	if !reflect.DeepEqual(choices, m.Choices) || !reflect.DeepEqual(before, m.Fields) || index != m.Index || value != m.Editor.Value() {
		t.Fatal("render mutated screen state")
	}
}
