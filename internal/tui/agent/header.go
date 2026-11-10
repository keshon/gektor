package agent

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/screen"
)

type headerState = screen.MenuState

type headerBadge = screen.Badge

func (m *Model) headerBadges() []headerBadge {
	mode, activity := "ACT", "READY"
	if m.plan {
		mode = "PLAN"
	}
	switch m.state {
	case stRunning:
		phase := "WAIT"
		switch m.progress.Phase {
		case "prefill":
			phase = "READ"
		case "reasoning":
			phase = "THINK"
		case "generating":
			phase = "STREAM"
		case "tool_call", "tools":
			phase = "TOOLS"
		case "unary":
			phase = "REPLY"
		case "retry":
			phase = "RETRY"
		case "checkpoint":
			phase = "COMPACT"
		}
		activity = string(activityFrames[m.spinnerFrame%len(activityFrames)]) + " " + phase
	case stPermission, stAsk:
		activity = "WAIT"
	case stStopping:
		activity = "STOP"
	default:
		if m.runErr != nil {
			activity = "ERROR"
		}
	}
	if m.evaluation != nil {
		activity = string(activityFrames[m.spinnerFrame%len(activityFrames)]) + " EVAL"
	}
	ctx := kTokens(m.tokens)
	if m.tokensEst {
		ctx = "~" + ctx
	}
	if m.limit > 0 {
		ctx += "/" + kTokens(m.limit)
	}
	width := m.transcript.Viewport.Width()
	candidates := []headerBadge{{ID: "mode", Label: mode}, {ID: "activity", Label: activity + "  " + ctx}}
	leftWidth := lipgloss.Width(mode) + lipgloss.Width(activity+"  "+ctx) + 4
	if m.steering {
		candidates = append(candidates, headerBadge{ID: "steering", Label: "STEER"})
		leftWidth += 7
	}
	run := headerBadge{ID: "run", Label: fmt.Sprintf("STEP %d/%s  %s", m.steps, m.stepBudget(), formatElapsed(m.elapsed))}
	runWidth := lipgloss.Width(run.Label) + 2
	showRun := leftWidth+runWidth <= width
	available := width - leftWidth - 2
	if showRun {
		available -= runWidth
	}
	if available >= 6 {
		candidates = append(candidates, headerBadge{ID: "model", Label: strings.TrimSpace(cellLine(modelLabel(m.displayedModel()), available))})
	}
	if showRun {
		candidates = append(candidates, run)
	}
	x := 0
	for i := range candidates {
		if candidates[i].ID == "run" {
			x = width - runWidth
		}
		candidates[i].Start = x
		candidates[i].End = min(x+lipgloss.Width(candidates[i].Label)+2, width)
		x = candidates[i].End
	}
	return candidates
}

func (m *Model) badgeLine() string {
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorPopupText)))
	badges := m.headerBadges()
	var out []string
	x := 0
	for _, b := range badges {
		out = append(out, strings.Repeat(" ", max(b.Start-x, 0)))
		x = b.End
		style := normal
		switch b.ID {
		case "model":
			if !m.usage.liveModel && (m.usage.last.Model != "" || m.usage.historical) {
				style = m.styles.Dim
			}
		case "steering":
			style = m.styles.Dim
		case "mode":
			style = style.Foreground(lipgloss.Color(themeColor(colorMode)))
		case "activity":
			color := colorReady
			if m.state != stDone || m.evaluation != nil {
				color = colorBusy
			}
			if m.runErr != nil {
				color = colorFailed
			}
			style = style.Foreground(lipgloss.Color(themeColor(color)))
		}
		if b.ID == "activity" {
			phase, context, _ := strings.Cut(b.Label, "  ")
			contextStyle := normal
			if m.usage.historical {
				contextStyle = m.styles.Dim
			} else if m.limit > 0 && float64(m.tokens)/float64(m.limit) >= 0.95 {
				contextStyle = m.styles.Err
			} else if m.limit > 0 && float64(m.tokens)/float64(m.limit) >= 0.85 {
				contextStyle = m.styles.Warn
			}
			out = append(out, style.Render(" "+phase)+m.styles.Dim.Render("  ")+contextStyle.Render(context+" "))
			continue
		}
		out = append(out, style.Render(" "+b.Label+" "))
	}
	return cellLine(strings.Join(out, ""), m.transcript.Viewport.Width())
}

func (m *Model) rawHeaderItems() []menuItem {
	if items := m.menuItems(m.chrome.Header.Menu); items != nil {
		return items
	}
	return m.badgeItems(m.chrome.Header.Menu)
}
func (m *Model) badgeItems(menu string) []menuItem {
	info := func(s string) menuItem { return menuItem{Text: s} }
	idle := m.state == stDone
	switch menu {
	case "steering":
		return []menuItem{{Text: "Steering", Command: "toggle-steering", Enabled: true}}
	case "mode":
		return []menuItem{{Text: "ACT   Execute changes with tools", Command: "/mode act", Enabled: idle}, {Text: "PLAN  Read-only exploration and planning", Command: "/mode plan", Enabled: idle}}
	case "activity":
		rows := []menuItem{info("State: " + m.statusWord()), info("Elapsed: " + formatElapsed(m.elapsed)), info(m.stepLine())}
		if m.progress.PromptTokens > 0 {
			rows = append(rows, info(fmt.Sprintf("Input: %s   Generated: %s", kTokens(m.progress.PromptTokens), kTokens(m.progress.CompletionTokens))))
			if m.progress.CachedTokens > 0 {
				rows = append(rows, info("Cached input: "+kTokens(m.progress.CachedTokens)))
			}
		}
		rows = append(rows, m.contextItems()...)
		if m.runErr != nil {
			rows = append(rows, info("Error: "+m.runErr.Error()), menuItem{Text: "Retry failed turn", Command: "/retry", Enabled: idle && m.retryRun != nil})
		}
		if m.busy() {
			rows = append(rows, menuItem{Text: "Stop run", Command: "stop", Enabled: m.state == stRunning})
		}
		return rows
	case "model":
		transport := "Request / response"
		if m.streaming {
			transport = "Streaming"
		}
		rows := []menuItem{info("Model: " + nonEmpty(m.displayedModel())), info("Backend: " + nonEmpty(m.backendKind)), info("Endpoint: " + nonEmpty(m.backendURL)), info("Transport: " + transport), info(fmt.Sprintf("Context window: %d tokens", m.limit)), info(fmt.Sprintf("Response budget: %d tokens", m.responseCap))}
		if !m.usage.liveModel && m.usage.historical {
			rows = append(rows, info("Current backend model: "+nonEmpty(m.modelName)))
		}
		return rows
	case "context":
		return m.contextItems()
	case "run":
		rows := []menuItem{info(m.stepLine()), info("Elapsed: " + formatElapsed(m.elapsed)), info(fmt.Sprintf("Tools: %d   Files: %d", m.toolsUsed, len(m.filesTouched)))}
		if !m.firstToken.IsZero() {
			rows = append(rows, info("First token: "+formatElapsed(m.firstToken.Sub(m.started))))
		}
		return rows
	}
	return nil
}

func (m *Model) contextItems() []menuItem {
	info := func(s string) menuItem { return menuItem{Text: s} }
	idle := m.state == stDone
	rows := []menuItem{info("Context: " + strings.Replace(m.meter(), "ctx ", "", 1)), info("Source: " + nonEmpty(m.usage.source))}
	if !m.usage.at.IsZero() {
		rows = append(rows, info("Updated: "+m.usage.at.Format("15:04:05")))
	}
	if m.usage.last.Tokens > 0 {
		kind := "reported"
		if m.usage.last.Estimated {
			kind = "estimated"
		}
		rows = append(rows, info(fmt.Sprintf("Last request: %s tokens (%s)", kTokens(m.usage.last.Tokens), kind)), info("Request time: "+m.usage.last.At.Format("Jan 2 15:04:05")))
	}
	if m.usage.source == "Saved history estimate" {
		rows = append(rows, info("History estimate includes images; tool schemas are excluded."))
	}
	return append(rows, menuItem{Text: "Compact saved history…", Command: "compact-session", Enabled: idle && m.stateFile != ""})
}

// Wrap informational rows so full model names, endpoints, and errors remain accessible.
func (m *Model) headerItems() []menuItem {
	m.syncMenus()
	return m.chrome.HeaderItems()
}

func (m *Model) closeHeader() {
	m.syncMenus()
	m.chrome.CloseHeader()
}
func (m *Model) headerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.syncMenus()
	command := m.chrome.HeaderKey(msg)
	if command == "quit" {
		return m.requestQuit()
	}
	if command == "interrupt" {
		m.quit = true
		m.cancel()
		return m, tea.Quit
	}
	if command != "" {
		return m.activateMenuAction(command)
	}
	return m, nil
}

func (m *Model) headerMenuItems() []popup.MenuItem {
	m.syncMenus()
	return m.chrome.HeaderMenuItems()
}

func (m *Model) headerMenuLayout() popup.MenuLayout {
	m.syncMenus()
	return m.chrome.HeaderMenuLayout()
}

func (m *Model) headerGeometry() (x, width, capacity, offset int) {
	m.syncMenus()
	return m.chrome.HeaderGeometry()
}

func (m *Model) overlayHeader(background string) string {
	m.syncMenus()
	return m.chrome.OverlayHeader(background)
}

func (m *Model) headerMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	m.syncMenus()
	handled, command := m.chrome.HeaderMouse(msg)
	if command != "" {
		_, cmd := m.activateMenuAction(command)
		return handled, cmd
	}
	return handled, nil
}

func (m *Model) normalizeHeader() {
	m.syncMenus()
	m.chrome.NormalizeHeader()
}

func (m *Model) menuCells() []footerCell {
	m.syncMenus()
	return m.chrome.MenuCells()
}
func (m *Model) headerNavigation() []headerBadge {
	m.syncMenus()
	return m.chrome.HeaderNavigation()
}
func (m *Model) menuLine() string {
	m.syncMenus()
	return m.chrome.MenuLine()
}

func (m *Model) headerTop() int {
	m.syncMenus()
	return m.chrome.HeaderTop()
}

func (m *Model) headerActionRow(start, delta int) int {
	m.syncMenus()
	return m.chrome.HeaderActionRow(start, delta)
}
