package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/config"
)

func (m *model) applyAppearance(a config.Appearance) {
	m.minimal = a.Frame != "outlined"
	m.dense = a.Density != "comfortable"
	m.hideNames, m.hideLabels = a.HideNames, a.HideLabels
	m.transcript.render.cache = nil
	m.fitBottom()
	m.refreshContent()
}

func (m *model) changeAppearance(change func(*config.Appearance)) error {
	a := config.Appearance{Frame: "outlined", Density: "comfortable", HideNames: m.hideNames, HideLabels: m.hideLabels}
	if m.minimal {
		a.Frame = "minimal"
	}
	if m.dense {
		a.Density = "compact"
	}
	change(&a)
	if m.application != nil && !m.application.Temporary {
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

// Only measured backend progress gets a determinate bar. Nothing is archived.
func (m *model) prefillStatus() string {
	p := m.progress
	if m.state != stRunning || m.dialog != nil || m.sessions != nil || p.Phase != "prefill" || p.Estimated || p.PromptTokens <= 0 || p.ProcessedTokens < 0 || m.termW < 40 || m.termH < 18 {
		return ""
	}
	processed := min(p.ProcessedTokens, p.PromptTokens)
	label := fmt.Sprintf("SYSTEM · Prompt processing %d%% · %d/%d tokens", int(float64(processed)*100/float64(p.PromptTokens)), processed, p.PromptTokens)
	width := max(m.transcript.Viewport.Width(), 1)
	if width < 60 {
		label = fmt.Sprintf("SYSTEM · Prefill %d%% · %d/%d", int(float64(processed)*100/float64(p.PromptTokens)), processed, p.PromptTokens)
	}
	filled := int(float64(processed) * float64(width) / float64(p.PromptTokens))
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorAccent))).Render(strings.Repeat("━", filled)) + m.styles.dim.Render(strings.Repeat("─", width-filled))
	return cellLine(label, width) + "\n" + bar
}
