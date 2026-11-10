package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
)

func TestSteeringCommandPersistsAndHeaderOnlyShowsEnabled(t *testing.T) {
	m, path := harnessModel(t)
	m.application.Preferences.Harness.MaxSteps = 42
	m.state = stRunning
	m.sess = &harness.Session{}
	for _, enabled := range []bool{true, false} {
		value := "off"
		if enabled {
			value = "on"
		}
		m.command("/steering " + value)
		if m.steering != enabled || strings.Contains(ansi.Strip(m.badgeLine()), "STEER") != enabled {
			t.Fatal("header does not reflect steering", m.badgeLine())
		}
		if m.dialog != nil || m.queued != "" {
			t.Fatal("steering requested a restart or queued")
		}
		p, err := config.LoadPreferences(path)
		if err != nil || p.Harness.Steering != enabled || p.Harness.MaxSteps != 42 {
			t.Fatal(p, err)
		}
	}
	if !runsImmediate("/steering on") {
		t.Fatal("steering queues during a run")
	}
}

func TestSteeringBadgeDoesNotOverlapMetrics(t *testing.T) {
	for _, width := range []int{40, 80, 160} {
		m := sizeModelTo(t, testModel(), width, 25)
		m.steering = true
		end := 0
		for _, b := range m.headerBadges() {
			if b.start < end || b.end > m.transcript.Viewport.Width() {
				t.Fatal(b)
			}
			end = b.end
		}
	}
}

func TestSkippedToolDisplayMatchesSavedHistory(t *testing.T) {
	m := sizeModel(t, testModel())
	call := llm.ToolCall{ID: "write", Name: "write_file"}
	history := []llm.Message{{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}}
	m.transcript.blocks = renderHistory(history)
	m.handleEvent(harness.Event{Name: "tool_finished", Fields: map[string]any{"call_id": "write"}})
	if !m.transcript.blocks[0].returned || !m.transcript.blocks[0].open {
		t.Fatal("preview closed authoritative result")
	}
	m.handleEvent(harness.Event{Name: "tool_result", Fields: map[string]any{"call_id": "write", "text": "Not executed", "failed": false, "status": "skipped_for_input"}})
	failed := false
	history = append(history, llm.Message{Role: llm.RoleTool, ToolCallID: "write", Content: "Not executed", ToolFailed: &failed, ToolStatus: "skipped_for_input"})
	restored := renderHistory(history)
	for _, b := range []block{m.transcript.blocks[0], restored[0]} {
		text := ansi.Strip(compactTool(b, m.styles, 100))
		if !strings.Contains(text, "Skipped for input") || strings.Contains(text, "✓") || b.failed || b.open {
			t.Fatal(text, b)
		}
	}
}
