package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/events"
	"github.com/keshon/gektor/internal/llm"
)

func TestAppearancePersistsWithoutChangingHistory(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.application = &Application{PreferencePath: filepath.Join(t.TempDir(), "application.json"), Preferences: config.Preferences{Version: 1}}
	m.appendBlock(userBlock("request"))
	m.appendBlock(markerBlock("harness instruction"))
	m.appendBlock(block{role: roleStatus, text: "local notice"})
	before := append([]block(nil), m.transcript.blocks...)
	if err := m.changeAppearance(func(a *config.Appearance) {
		a.Frame = "minimal"
		a.HideNames = true
		a.HideLabels = true
		a.Density = "compact"
	}); err != nil {
		t.Fatal(err)
	}
	p, err := config.LoadPreferences(m.application.PreferencePath)
	if err != nil {
		t.Fatal(err)
	}
	n := sizeModel(t, testModel())
	n.applyAppearance(p.Appearance)
	if !n.minimal || !n.hideNames || !n.hideLabels || !n.dense {
		t.Fatal("appearance not restored")
	}
	if !reflect.DeepEqual(before, m.transcript.blocks) {
		t.Fatal("display mutated history")
	}
	text := ansi.Strip(m.transcript.Viewport.View())
	if strings.Contains(text, "SYSTEM") || !strings.Contains(text, "│ harness instruction") || !strings.Contains(text, "│ local notice") {
		t.Fatal(text)
	}
	if err := m.changeAppearance(func(a *config.Appearance) { a.HideNames = false }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ansi.Strip(m.transcript.Viewport.View()), "SYSTEM") || !m.hideLabels {
		t.Fatal("names and labels are coupled")
	}
}

func TestBothFramesAndMeasuredPrefillFit(t *testing.T) {
	for _, frame := range []string{"minimal", "outlined"} {
		for _, size := range [][2]int{{120, 34}, {80, 24}, {40, 18}, {20, 10}} {
			m := sizeModelTo(t, testModel(), size[0], size[1])
			m.state = stRunning
			m.applyAppearance(config.Appearance{Frame: frame, HideNames: true})
			m.appendBlock(userBlock("Check 界 text"))
			m.appendBlock(answerBlock("I’ll inspect the configuration first."))
			m.appendBlock(toolCardBlock("read", `read_file {"path":"config.go"}`))
			m.attachResult("read", "configuration loaded", false)
			m.appendBlock(block{role: roleStatus, text: "processing"})
			m.handleEvent(events.Event{Name: "progress", Fields: map[string]any{"phase": "prefill", "prompt": float64(12288), "processed": float64(8897)}})
			text := ansi.Strip(m.View().Content)
			if m.transcript.Follow && !m.transcript.Viewport.AtBottom() {
				t.Fatal("progress resize lost latest content")
			}
			if size[0] >= 40 && (!strings.Contains(text, "Session") || !strings.Contains(text, "F1")) {
				t.Fatal("menu/hotkeys lost", frame, size, text)
			}
			for _, line := range strings.Split(text, "\n") {
				if lipgloss.Width(line) > size[0] {
					t.Fatal(frame, size, line)
				}
			}
			if len(strings.Split(text, "\n")) != size[1] {
				t.Fatal("frame height", frame, size, text)
			}
			if size[0] >= 40 && !strings.Contains(text, "8897/12288") {
				t.Fatal("measured counts absent", text)
			}
			if dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR"); dir != "" {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("refinement-%s-%dx%d.txt", frame, size[0], size[1])), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			height := m.transcript.Viewport.Height()
			m.handleEvent(events.Event{Name: "reasoning", Fields: map[string]any{"text": "thinking"}})
			if size[0] >= 40 && m.transcript.Viewport.Height() != height+1 {
				t.Fatal("prefill row not reclaimed")
			}
			m.progress = llm.Progress{Phase: "prefill", PromptTokens: 12288, ProcessedTokens: 8897}
			m.progress.Estimated = true
			if m.prefillStatus() != "" {
				t.Fatal("estimated determinate progress")
			}
			m.progress = llm.Progress{Phase: "reasoning"}
			m.fitBottom()
			if strings.Contains(ansi.Strip(m.View().Content), "Prompt processing") {
				t.Fatal("stale progress")
			}
			m.openDialog("Inspect", []string{"evidence"})
			if m.prefillStatus() != "" {
				t.Fatal("progress behind dialog")
			}
			m.closeDialog()
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		}
	}
}

func TestOutcomeReceiptPreservesMeasuredStates(t *testing.T) {
	for _, outcome := range []string{"completed", "failed", "cancelled", "incomplete"} {
		m := sizeModel(t, testModel())
		m.hideNames = true
		m.hideLabels = true
		m.handleEvent(events.Event{Name: "outcome", Fields: map[string]any{"outcome": outcome, "files": []any{"main.go"}, "verification": "checks failed: assertion X", "resume": "session-1"}})
		b := m.transcript.blocks[len(m.transcript.blocks)-1]
		if b.role != roleReceipt || b.contextText != "" {
			t.Fatal("receipt changed delivery metadata", b)
		}
		text := ansi.Strip(m.transcript.Viewport.View())
		for _, part := range []string{"│ outcome: " + outcome, "main.go", "checks failed: assertion X", "gektor -tui -resume session-1"} {
			if !strings.Contains(text, part) {
				t.Fatal("receipt lost evidence", part, text)
			}
		}
	}
	m := sizeModel(t, testModel())
	m.handleEvent(events.Event{Name: "outcome", Fields: map[string]any{"outcome": "completed"}})
	if !strings.Contains(m.transcript.blocks[0].text, "no current command verdict") {
		t.Fatal("unverified completion became verified")
	}
}
