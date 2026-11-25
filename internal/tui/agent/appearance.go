package agent

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// ApplyHistoryAppearance updates Agent display options and refreshes the screen.
func (m *Model) ApplyHistoryAppearance(a config.Appearance) {
	thinkingExpanded := !m.transcript.compact
	if thinkingExpanded != a.ExpandThinking || m.hideThinking != a.HideThinking || m.hideTools != a.HideTools || m.toolNamesOnly != a.ToolNamesOnly {
		m.transcript.expanded = nil
	}
	m.hideNames, m.hideLabels = a.HideNames, a.HideLabels
	m.hideSYS, m.hideTools = a.HideSystem, a.HideTools
	m.transcript.compact = !a.ExpandThinking
	m.hideThinking, m.toolNamesOnly = a.HideThinking, a.ToolNamesOnly
	m.RefreshTheme()
	m.fitBottom()
	m.refreshContent()
}

func (m *Model) currentAppearance() config.Appearance {
	palette := currentPalette()
	a := config.Appearance{
		Caret:          palette.Caret,
		Frame:          "outlined",
		HideNames:      m.hideNames,
		HideLabels:     m.hideLabels,
		HideSystem:     m.hideSYS,
		HideTools:      m.hideTools,
		ExpandThinking: !m.transcript.compact,
		HideThinking:   m.hideThinking,
		ToolNamesOnly:  m.toolNamesOnly,
	}
	if !palette.Outlined {
		a.Frame = "minimal"
	}
	if palette.Modern {
		a.Style = "modern"
	} else {
		a.Style = "commander"
	}
	return a
}

func (m *Model) changeAppearance(change func(*config.Appearance)) error {
	a := m.currentAppearance()
	change(&a)
	err := m.application.SaveAppearance(a)
	if err != nil {
		return err
	}
	m.ApplyHistoryAppearance(a)
	return nil
}

func (m *Model) toggleThinking() (tea.Model, tea.Cmd) {
	err := m.changeAppearance(func(a *config.Appearance) {
		switch {
		case a.HideThinking:
			a.HideThinking, a.ExpandThinking = false, false
		case a.ExpandThinking:
			a.HideThinking, a.ExpandThinking = true, false
		default:
			a.ExpandThinking = true
		}
	})
	if err != nil {
		m.chrome.Open("Appearance", []string{err.Error()})
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
	bar := st.Dim.Render(strings.Repeat("─", position)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorMode))).Render(strings.Repeat("━", span)) +
		st.Dim.Render(strings.Repeat("─", width-position-span))
	return heading + "\n" + st.Dim.Render(strings.Join(reflow(label, width), "\n")) + "\n" + bar
}

func prefillText(p llm.Progress, st styles, width int) string {
	width = max(width, 1)
	total := max(p.PromptTokens, 0)
	processed := min(max(p.ProcessedTokens, 0), total)
	label := fmt.Sprintf("Cached unknown"+screen.Separator()+"Prompt %d/%d", processed, total)
	if width < lipgloss.Width(label) {
		label = fmt.Sprintf("Prompt %d/%d"+screen.Separator()+"Cached ?", processed, total)
	}
	filled := 0
	// llama.cpp processed includes cache; only the suffix is new work.
	if p.CacheKnown && p.CachedTokens >= 0 && p.CachedTokens <= processed {
		cached := p.CachedTokens
		work, done := total-cached, processed-cached
		label = fmt.Sprintf("Cached %s"+screen.Separator()+"Processing %d/%d", kTokens(cached), done, work)
		if work == 0 {
			label = fmt.Sprintf("Cached %s"+screen.Separator()+"No new input to process", kTokens(cached))
			filled = width
		} else {
			filled = int(float64(done) * float64(width) / float64(work))
		}
		if width < lipgloss.Width(label) {
			label = fmt.Sprintf("Cached %s"+screen.Separator()+"New %s/%s", kTokens(cached), kTokens(done), kTokens(work))
		}
	} else if total > 0 {
		filled = int(float64(processed) * float64(width) / float64(total))
	}
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorMode))).Render(strings.Repeat("━", filled)) + st.Dim.Render(strings.Repeat("─", width-filled))
	return st.Dim.Render(cellLine(label, width)) + "\n" + bar
}

func (m *Model) thinkingLabel() string {
	if m.hideThinking {
		return "Hidden"
	}
	if m.transcript.compact {
		return "Collapsed"
	}
	return "Expanded"
}

func (m *Model) toolsLabel() string {
	if m.toolNamesOnly {
		return "Badges"
	}
	if m.hideTools {
		return "Per line"
	}
	return "Expanded"
}
