package agent

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
)

func (m *Model) applyAppearance(a config.Appearance) {
	m.minimal = a.Frame != "outlined"
	m.hideNames, m.hideLabels = a.HideNames, a.HideLabels
	m.hideSYS, m.hideTools = a.HideSystem, a.HideTools
	m.transcript.compact = !a.ExpandThinking
	m.transcript.render.cache = nil
	m.fitBottom()
	m.refreshContent()
}

func (m *Model) currentAppearance() config.Appearance {
	a := config.Appearance{Frame: "outlined", HideNames: m.hideNames, HideLabels: m.hideLabels, HideSystem: m.hideSYS, HideTools: m.hideTools, ExpandThinking: !m.transcript.compact}
	if m.minimal {
		a.Frame = "minimal"
	}
	return a
}

func (m *Model) changeAppearance(change func(*config.Appearance)) error {
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

func (m *Model) toggleThinking() (tea.Model, tea.Cmd) {
	if err := m.changeAppearance(func(a *config.Appearance) { a.ExpandThinking = !a.ExpandThinking }); err != nil {
		m.openDialog("Appearance", []string{err.Error()})
	}
	return m, nil
}

// Compaction remains visible even before the backend reports token counts.
func (m *Model) prefillActive() bool {
	p := m.progress
	visible := p.Phase == "checkpoint" || (p.Phase == "prefill" && !p.Estimated && p.PromptTokens > 0 && p.ProcessedTokens >= 0)
	return m.state == stRunning && !m.hideSYS && visible && m.termW >= 40 && m.termH >= 18
}

func historyProgressText(p llm.Progress, st styles, width, frame int) string {
	width = max(width, 1)
	phase := "READ"
	if p.Phase == "checkpoint" {
		phase = "COMPACT"
	}
	heading := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorBusy))).Render(phase)
	if p.Phase != "checkpoint" || (p.Subphase == "prefill" && !p.Estimated && p.PromptTokens > 0) {
		return heading + "\n" + prefillText(p, st, width)
	}
	label := "Summarizing earlier steps"
	if p.CompletionTokens > 0 {
		label += fmt.Sprintf(" (%d tokens generated)", p.CompletionTokens)
	}
	span := max(width/8, 1)
	position := (width - span) * frame / max(len(activityFrames)-1, 1)
	bar := st.dim.Render(strings.Repeat("─", position)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorMode))).Render(strings.Repeat("━", span)) +
		st.dim.Render(strings.Repeat("─", width-position-span))
	return heading + "\n" + st.dim.Render(strings.Join(reflow(label, width), "\n")) + "\n" + bar
}

func prefillText(p llm.Progress, st styles, width int) string {
	width = max(width, 1)
	total := max(p.PromptTokens, 0)
	processed := min(max(p.ProcessedTokens, 0), total)
	label := fmt.Sprintf("Cached unknown · Prompt %d/%d", processed, total)
	filled := 0
	// llama.cpp processed includes cache; only the suffix is new work.
	if p.CacheKnown && p.CachedTokens >= 0 && p.CachedTokens <= processed {
		cached := p.CachedTokens
		work, done := total-cached, processed-cached
		label = fmt.Sprintf("Cached %s · Processing %d/%d", kTokens(cached), done, work)
		if work == 0 {
			label = fmt.Sprintf("Cached %s · No new input to process", kTokens(cached))
			filled = width
		} else {
			filled = int(float64(done) * float64(width) / float64(work))
		}
		if width < lipgloss.Width(label) {
			label = fmt.Sprintf("Cached %s · New %s/%s", kTokens(cached), kTokens(done), kTokens(work))
		}
	} else if total > 0 {
		filled = int(float64(processed) * float64(width) / float64(total))
	}
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorMode))).Render(strings.Repeat("━", filled)) + st.dim.Render(strings.Repeat("─", width-filled))
	return st.dim.Render(cellLine(label, width)) + "\n" + bar
}

// openAppearance stages presentation choices and saves them together on Apply.
func (m *Model) openAppearance() {
	m.openForm("Appearance", "Apply saves these choices for future launches.", "Apply", []formField{
		{Key: "theme", Label: "Theme", Value: currentPalette().name},
		{Key: "frame", Label: "Panel frame", Value: m.currentAppearance().Frame},
	}, func(values map[string]string) error {
		theme, frame := values["theme"], values["frame"]
		if !config.ValidTheme(theme) {
			return fmt.Errorf("choose a theme from the list")
		}
		if frame != "outlined" && frame != "minimal" {
			return fmt.Errorf("choose an outlined or minimal frame")
		}
		appearance := m.currentAppearance()
		appearance.Frame = frame
		if m.application != nil {
			preferences, err := config.LoadPreferences(m.application.PreferencePath)
			if err != nil {
				return err
			}
			preferences.Theme, preferences.Appearance = theme, appearance
			if err := config.SavePreferences(m.application.PreferencePath, preferences); err != nil {
				return err
			}
			m.application.Preferences.Theme, m.application.Preferences.Appearance = theme, appearance
		}
		m.applyAppearance(appearance)
		m.applyTheme(theme)
		return nil
	})
	m.dialog.form.Choices = map[string][]string{"theme": config.ThemeNames(), "frame": {"outlined", "minimal"}}
}
