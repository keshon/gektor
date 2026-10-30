package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
)

func (m *model) applyAppearance(a config.Appearance) {
	m.minimal = a.Frame != "outlined"
	m.hideNames, m.hideLabels = a.HideNames, a.HideLabels
	m.hideSYS, m.hideTools = a.HideSystem, a.HideTools
	m.transcript.compact = !a.ExpandThinking
	m.transcript.render.cache = nil
	m.fitBottom()
	m.refreshContent()
}

func (m *model) currentAppearance() config.Appearance {
	a := config.Appearance{Frame: "outlined", HideNames: m.hideNames, HideLabels: m.hideLabels, HideSystem: m.hideSYS, HideTools: m.hideTools, ExpandThinking: !m.transcript.compact}
	if m.minimal {
		a.Frame = "minimal"
	}
	return a
}

func (m *model) changeAppearance(change func(*config.Appearance)) error {
	a := m.currentAppearance()
	change(&a)
	if m.application != nil {
		p, err := config.LoadPreferences(m.application.PreferencePath)
		if err != nil {
			return err
		}
		p.Appearance = a
		if err := config.SavePreferences(m.application.PreferencePath, p); err != nil {
			return err
		}
		m.application.Preferences.Appearance = a
	}
	m.applyAppearance(a)
	return nil
}

func (m *model) toggleThinking() (tea.Model, tea.Cmd) {
	if err := m.changeAppearance(func(a *config.Appearance) { a.ExpandThinking = !a.ExpandThinking }); err != nil {
		m.openDialog("Appearance", []string{err.Error()})
	}
	return m, nil
}

// Only measured backend progress gets a transient history block.
func (m *model) prefillActive() bool {
	p := m.progress
	return m.state == stRunning && !m.hideSYS && p.Phase == "prefill" && !p.Estimated && p.PromptTokens > 0 && p.ProcessedTokens >= 0 && m.termW >= 40 && m.termH >= 18
}

func prefillText(p llm.Progress, st styles, width int) string {
	processed := min(p.ProcessedTokens, p.PromptTokens)
	label := fmt.Sprintf("Server-reported prompt processing %d%% · %d/%d tokens", int(float64(processed)*100/float64(p.PromptTokens)), processed, p.PromptTokens)
	if width < 60 {
		label = fmt.Sprintf("Prefill %d%% · %d/%d", int(float64(processed)*100/float64(p.PromptTokens)), processed, p.PromptTokens)
	}
	filled := int(float64(processed) * float64(width) / float64(p.PromptTokens))
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorMode))).Render(strings.Repeat("━", filled)) + st.dim.Render(strings.Repeat("─", width-filled))
	return st.dim.Render(cellLine(label, width)) + "\n" + bar
}
