package tui

import (
	"strings"
	"testing"

	"github.com/keshon/tars/internal/config"
)

// Cards belong to Chat. An Agent takes no character: the card is never
// read, so the Character page and menu row would be a setting that saves
// a value nothing consumes.
func TestCharacterSettingsAreChatOnly(t *testing.T) {
	t.Run("agent", func(t *testing.T) {
		m := agentSettingsModel(t) // Agent mode
		if m.chat != nil {
			t.Fatal("setup: expected Agent mode")
		}
		m.header.menu = "settings"
		for _, item := range m.rawHeaderItems() {
			if item.command == "edit:character" {
				t.Fatalf("Agent offers the Character page: %+v", item)
			}
		}
		m.editSettings("character")
		for _, c := range m.dialog.form.AvailableCategories() {
			if c.ID == "character" {
				t.Fatal("Agent category rail offers Character")
			}
		}
	})
	t.Run("chat", func(t *testing.T) {
		m := settingsModel(t) // Chat mode
		if m.chat == nil {
			t.Fatal("setup: expected Chat mode")
		}
		m.header.menu = "settings"
		found := false
		for _, item := range m.rawHeaderItems() {
			if item.command == "edit:character" {
				found = true
			}
		}
		if !found {
			t.Fatal("Chat lost the Character page")
		}
		m.editSettings("character")
		for _, c := range m.dialog.form.AvailableCategories() {
			if c.ID == "character" {
				return
			}
		}
		t.Fatal("Chat category rail lost Character")
	})
}

// Switching an Agent profile that names a missing card must not fail.
// Only Chat reads a card, so only Chat can be broken by one.
func TestAgentProfileDoesNotRequireAReadableCard(t *testing.T) {
	m := agentSettingsModel(t)
	m.audioConfig.Profile.Conversation = &config.Conversation{
		UserName: "User", SystemPrompt: config.DefaultSystemPrompt,
		Character: "C:/definitely/not/here/card.json",
	}
	m.application.Preferences.Mode = "agent"
	m.editSettings("application")
	m.dialog.form.Editor.SetValue("chat")
	focusSetting(m, "app-voice")
	m.applyForm()
	if m.dialog != nil && strings.Contains(m.dialog.form.Error, "character") {
		t.Fatalf("Agent refused a card it will never read: %q", m.dialog.form.Error)
	}
}