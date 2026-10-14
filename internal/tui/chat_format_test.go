package tui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestChatConversationFormatting(t *testing.T) {
	st := defaultStyles()
	text := "\"Hello\" *she smiles*\n> **quoted words**"
	for _, out := range []string{renderBlock(answerBlock(text), st, false, 80, true), strings.Join(renderLive(text, st, 80, true), "\n")} {
		plain := ansi.Strip(out)
		if !strings.HasPrefix(plain, "TARS  \"Hello\"") {
			t.Fatalf("chat reply does not share the speaker row: %q", plain)
		}
		if strings.Contains(plain, "[RESPONSE]") || strings.Contains(plain, "*she smiles*") || !strings.Contains(plain, "\"Hello\" she smiles") || !strings.Contains(plain, "│ quoted words") {
			t.Fatalf("chat formatting: %q", plain)
		}
	}
	if !strings.Contains(ansi.Strip(renderBlock(answerBlock("hello"), st, false, 80)), "[RESPONSE]") {
		t.Fatal("agent response label lost")
	}
}
