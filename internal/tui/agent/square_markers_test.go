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
	}
}

func TestStatusAnimationsKeepBadgePositions(t *testing.T) {
	previous := screen.CurrentPalette().Appearance
	defer screen.SetAppearance(previous)
	for _, modern := range []bool{false, true} {
		for _, choice := range []string{"braille", "square", "text"} {
			m := sizeModelTo(t, testModel(), 80, 30)
			screen.SetAppearance(screen.Appearance{Modern: modern, StatusAnimation: choice})
			m.state, m.progress.Phase = stRunning, "prefill"
			m.tokens, m.limit = 1200, 32000

			onBadges := m.headerBadges()
			on := ansi.Strip(m.badgeLine(onBadges))
			m.spinnerFrame = len(activityFrames) / 2
			offBadges := m.headerBadges()
			off := ansi.Strip(m.badgeLine(offBadges))
			for i, badge := range onBadges {
				if badge.Start != offBadges[i].Start || badge.End != offBadges[i].End {
					t.Fatal("badges moved during animation", modern, choice)
				}
			}
			onContext := strings.Index(on, "1.2k/32.0k")
			offContext := strings.Index(off, "1.2k/32.0k")
			if on == off || onContext < 0 || offContext < 0 || ansi.StringWidth(on[:onContext]) != ansi.StringWidth(off[:offContext]) {
				t.Fatal("animation did not change or moved context", modern, choice, on, off)
			}
			switch choice {
			case "braille":
				if !strings.Contains(on, "⠋ READ") || !strings.Contains(off, "⠴ READ") {
					t.Fatal("Braille spinner lost its frames", on, off)
				}
			case "square":
				if !strings.Contains(on, "▪ READ") || !strings.Contains(off, " READ") || strings.ContainsAny(off, "▪▫") {
					t.Fatal("square should blink between filled and absent", on, off)
				}
			case "text":
				if !strings.Contains(on, "READ") || strings.Contains(off, "READ") || strings.ContainsAny(on, "▪▫⠋") {
					t.Fatal("text should blink without a symbol", on, off)
				}
			}
		}
	}
}
