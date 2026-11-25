package agent

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/pkg/tui/components/transcript"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestModernMarkersPreserveContentPunctuation(t *testing.T) {
	previous := screen.CurrentPalette()
	defer func() { screen.SetModern(previous.Modern) }()
	for _, modern := range []bool{false, true} {
		screen.SetModern(modern)
		st := defaultStyles()
		progress := ansi.Strip(prefillText(llm.Progress{CacheKnown: true, PromptTokens: 100, ProcessedTokens: 20}, st, 80))
		marker := " · "
		if modern {
			marker = " ▪ "
		}
		if !strings.Contains(progress, "Cached 0"+marker+"Processing 20/100") {
			t.Fatal(progress)
		}
		raw := "- prose · stays; file.go v1.2\n\n```text\n• · ○ ●\n```"
		b := answerBlock(raw)
		rendered := ansi.Strip(strings.Join(renderMdBlock(b, raw, st, 80), "\n"))
		bullet := "•"
		if modern {
			bullet = "▪"
		}
		if !strings.Contains(rendered, bullet+" prose · stays; file.go v1.2") || !strings.Contains(rendered, "• · ○ ●") || b.text != raw {
			t.Fatal("source content changed", rendered)
		}
		returned := ansi.Strip(transcript.ActionGlyph(transcript.ActionReturned, toolActionStyles(st)))
		if returned != strings.TrimSpace(marker) {
			t.Fatal("returned marker differs", modern, returned)
		}
		var model Model
		for i := 0; i < len(activityFrames); i++ {
			model.spinnerFrame = i
			glyph := model.activitySymbol()
			if modern && glyph != "▪" && glyph != "▫" {
				t.Fatal("round spinner in Modern", glyph)
			}
			if !modern && glyph != string(activityFrames[i]) {
				t.Fatal("Commander spinner changed", glyph)
			}
		}
	}
}
