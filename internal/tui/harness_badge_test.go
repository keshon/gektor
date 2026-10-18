package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/tars/internal/agent"
)

// The harness level is only actionable if it is visible while a run is
// happening. It was reachable only by remembering the slash command, which
// made it read as a setting with no state.
func TestHeaderShowsTheHarnessLevelBesideSteps(t *testing.T) {
	m := sizeModel(t, testModel())
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.state = stRunning
	m.steps = 3

	for _, level := range []agent.NudgeLevel{agent.NudgeFull, agent.NudgeRecovery, agent.NudgeOff} {
		m.nudges = level
		view := ansi.Strip(m.View().Content)
		if !strings.Contains(view, "harness "+agent.NudgeLevelName(level)) {
			t.Errorf("header does not show %q:\n%s", agent.NudgeLevelName(level), firstLines(view, 6))
		}
		// It has to sit by the step counter, not somewhere off on its own.
		if stepAt, harnessAt := strings.Index(view, "step 3/"), strings.Index(view, "harness "); stepAt >= 0 {
			if harnessAt < stepAt {
				t.Errorf("the harness badge precedes the step counter")
			}
		}
	}
}

func TestHarnessBadgeOpensItsOwnChoices(t *testing.T) {
	m := sizeModel(t, testModel())
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.nudges = agent.NudgeRecovery
	m.header.menu = "harness"
	items := m.headerItems()
	var commands []string
	for _, it := range items {
		if it.command != "" {
			commands = append(commands, it.command)
		}
	}
	if len(commands) == 0 {
		t.Fatal("the harness badge has no menu")
	}
	joined := strings.Join(commands, " ")
	for _, want := range []string{"/nudges full", "/nudges recovery", "/nudges off"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no way to choose %q from the header: %v", want, commands)
		}
	}
	// The level already in force stays listed, disabled: the menu is how a
	// reader confirms what is set, not just a way to change it.
	found := false
	for _, it := range items {
		if it.command != "/nudges "+agent.NudgeLevelName(m.nudges) {
			continue
		}
		found = true
		if it.enabled {
			t.Errorf("the level in force is offered as a change: %q", it.command)
		}
	}
	if !found {
		t.Errorf("the menu does not say which level is in force: %v", commands)
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
