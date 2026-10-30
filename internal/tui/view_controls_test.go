package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/events"
)

func TestThinkingSummaryUsesOneWindowWidthRow(t *testing.T) {
	thought := strings.Repeat("Check 界 condition.\n", 40)
	for _, width := range []int{20, 40, 80, 140} {
		summary := ansi.Strip(renderBlock(thinkBlock(thought), defaultStyles(), renderOpts{}, width))
		if strings.Contains(summary, "\n") || lipgloss.Width(summary) != width {
			t.Fatal("thinking summary does not fill exactly one row", width, summary)
		}
		full := ansi.Strip(renderBlock(thinkBlock(thought), defaultStyles(), renderOpts{expand: true}, width))
		if !strings.Contains(full, "\n") || strings.Contains(full, "chars]") {
			t.Fatal("full thinking still truncated")
		}
	}
}

func TestPrefillAndSystemRailMatchHistoryInBothThemes(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range []string{"dark", "light"} {
		for _, frame := range []string{"minimal", "outlined"} {
			m := sizeModelTo(t, testModel(), 120, 34)
			m.applyTheme(theme)
			m.applyAppearance(config.Appearance{Frame: frame, HideNames: true, HideLabels: true})
			m.state = stRunning
			m.appendBlock(userBlock("Trace the browser lifecycle before proposing a change."))
			m.appendBlock(answerBlock("I’ll inspect launch, activation and profile cleanup."))
			before := append([]block(nil), m.transcript.blocks...)
			m.handleEvent(events.Event{Name: "progress", Fields: map[string]any{"phase": "prefill", "prompt": float64(12288), "processed": float64(8897)}})
			view := m.View().Content
			rows := strings.Split(ansi.Strip(view), "\n")
			progressRow := -1
			for y, line := range rows {
				if strings.Contains(line, "Server-reported prompt processing 72%") {
					progressRow = y
				}
			}
			if progressRow < paneTop || progressRow+1 >= paneTop+m.transcript.Viewport.Height() || strings.Contains(ansi.Strip(m.statusLine()), "processing") {
				t.Fatal("progress is not entirely in history", theme, frame, progressRow)
			}
			screen := uv.NewScreenBuffer(m.termW, m.termH)
			uv.NewStyledString(view).Draw(screen, uv.Rect(0, 0, m.termW, m.termH))
			for _, pos := range [][2]int{{m.promptOffset(), progressRow}, {m.promptOffset(), progressRow + 1}, {m.promptOffset() + 2, progressRow + 1}} {
				if c := screen.CellAt(pos[0], pos[1]); c == nil || !sameColor(c.Style.Fg, themeColor(colorMode)) {
					t.Fatal("SYSTEM rail/progress does not use ACT color", theme, frame, pos)
				}
			}
			if !reflect.DeepEqual(before, m.transcript.blocks) {
				t.Fatal("transient progress changed history")
			}
			if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "view-"+theme+"-"+frame+".ansi"), []byte(view), 0600); err != nil {
					t.Fatal(err)
				}
			}
			m.handleEvent(events.Event{Name: "progress", Fields: map[string]any{"phase": "generating"}})
			if strings.Contains(ansi.Strip(m.View().Content), "prompt processing") {
				t.Fatal("stale progress remained in history")
			}
			m.appendBlock(block{role: roleStatus, text: "SYSTEM_NOTICE"})
			m.hideSYS = true
			m.refreshContent()
			if strings.Contains(ansi.Strip(m.View().Content), "SYSTEM_NOTICE") {
				t.Fatal("system toggle did not filter local notices")
			}
		}
	}
}

func TestGektorDefaultAttributionAndCommandsHeading(t *testing.T) {
	m := sizeModelTo(t, testModel(), 120, 34)
	m.identityName = "Assistant"
	m.appendBlock(answerBlock("reply"))
	if text := ansi.Strip(m.View().Content); !strings.Contains(text, "GEKTOR") || strings.Contains(text, "ASSISTANT") {
		t.Fatal("legacy default speaker remains", text)
	}
	m.identityName = "Reviewer"
	m.refreshContent()
	if !strings.Contains(ansi.Strip(m.View().Content), "REVIEWER") {
		t.Fatal("custom identity was overwritten")
	}
	m.openHelp()
	m.dialog.title = "Commands"
	text := ansi.Strip(m.dialogView())
	if strings.Contains(text, "[Commands]") || strings.Count(text, "Commands") != 1 {
		t.Fatal("duplicate Commands heading", text)
	}
}
