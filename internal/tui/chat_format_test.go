package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"strings"
	"testing"
)

func TestChatConversationFormatting(t *testing.T) {
	st := defaultStyles()
	text := "\"Hello\" *she smiles*\n> **quoted words**"
	for _, out := range []string{renderBlock(answerBlock(text), st, false, 80, true), strings.Join(renderLive(text, st, 80, true), "\n")} {
		plain := ansi.Strip(out)
		if !strings.HasPrefix(plain, "Assistant:\n\"Hello\"") {
			t.Fatalf("chat reply does not use a separate speaker row: %q", plain)
		}
		if strings.Contains(plain, "[RESPONSE]") || strings.Contains(plain, "*she smiles*") || !strings.Contains(plain, "\"Hello\" she smiles") || !strings.Contains(plain, "│ quoted words") {
			t.Fatalf("chat formatting: %q", plain)
		}
	}
	if !strings.Contains(ansi.Strip(renderBlock(answerBlock("hello"), st, false, 80)), "[RESPONSE]") {
		t.Fatal("agent response label lost")
	}
}

func TestChatCharacterSpeaker(t *testing.T) {
	b := answerBlock("Hello.")
	b.speaker = "Veyra"
	st := defaultStyles()
	got := ansi.Strip(renderBlock(b, st, false, 80, true))
	if !strings.HasPrefix(got, "Veyra:\nHello.") || strings.Contains(got, "TARS") {
		t.Fatal(got)
	}
	got = ansi.Strip(renderBlock(b, st, false, 80, false))
	if !strings.Contains(got, "Veyra") {
		t.Fatal(got)
	}
}

func TestCharacterLabelForCompletedAndStreamingChat(t *testing.T) {
	m := sizeModel(t, testModel())
	m.chat = &chatUI{config: ChatConfig{Settings: chat.Config{CharacterName: "Veyra"}}}
	m.blocks = []block{answerBlock("Completed hello.")}
	m.live = "Streaming hello."
	m.refreshContent()
	got := ansi.Strip(m.vp.View())
	if strings.Count(got, "Veyra") != 2 || strings.Contains(got, "TARS") {
		t.Fatal(got)
	}
	m.chat.config.Settings.CharacterName = "Another"
	m.refreshContent()
	got = ansi.Strip(m.vp.View())
	if strings.Count(got, "Another") != 2 || strings.Contains(got, "Veyra") {
		t.Fatal(got)
	}
}

func TestAgentIdentityAndLongChatLabel(t *testing.T) {
	m := sizeModel(t, testModel())
	m.identityName = "Ada"
	m.blocks = []block{answerBlock("Reply"), {role: roleThink, text: "Thinking"}, {role: roleTool, text: "read_file"}}
	m.refreshContent()
	got := ansi.Strip(m.vp.View())
	if strings.Contains(got, "TARS") || strings.Count(got, "Ada") != 3 {
		t.Fatal(got)
	}
	b := answerBlock("Long text that must wrap under this name.")
	b.speaker = "An extremely long character name"
	for _, line := range strings.Split(renderBlock(b, defaultStyles(), false, 24, true), "\n") {
		if ansi.StringWidth(line) > 24 {
			t.Fatalf("overflow: %q", line)
		}
	}
}

func TestNamedUserAndAssistantHaveSeparateRows(t *testing.T) {
	m := sizeModel(t, testModel())
	m.chat = &chatUI{config: ChatConfig{Settings: chat.Config{CharacterName: "An unusually long character name"}, Profile: config.Profile{Conversation: &config.Conversation{UserName: "Alex"}}}}
	m.blocks = []block{userBlock("My words"), answerBlock("Her words")}
	m.refreshContent()
	plain := ansi.Strip(m.vp.View())
	if !strings.Contains(plain, "Alex:") || strings.Contains(plain, "You:") || !strings.Contains(plain, "An unusually long character name:") {
		t.Fatal(plain)
	}
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "Alex:") && strings.Contains(line, "My words") {
			t.Fatal("user name and body still share a row")
		}
	}
	m.chat.config.Profile.Conversation.UserName = "User"
	m.refreshContent()
	if !strings.Contains(ansi.Strip(m.vp.View()), "User:") {
		t.Fatal("configured default user ignored")
	}
}

func TestEmphasisUsesThemeSecondaryGrey(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"dark", "light"} {
		setTheme(theme)
		got := renderBlock(answerBlock("Normal *gesture* normal"), defaultStyles(), false, 80, true)
		expected := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorSecondaryText))).Italic(true).Render("gesture")
		if !strings.Contains(got, expected) {
			t.Fatal(theme, got)
		}
	}
}
