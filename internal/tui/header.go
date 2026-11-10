package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

type headerState struct {
	global  bool
	focused bool
	index   int
	menu    string
	row     int
}

type headerBadge struct {
	id, label  string
	start, end int
}

func (m *model) headerBadges() []headerBadge {
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
	candidates := []headerBadge{{id: "mode", label: mode}, {id: "activity", label: activity + "  " + ctx}}
	leftWidth := lipgloss.Width(mode) + lipgloss.Width(activity+"  "+ctx) + 4
	if m.steering {
		candidates = append(candidates, headerBadge{id: "steering", label: "STEER"})
		leftWidth += 7
	}
	run := headerBadge{id: "run", label: fmt.Sprintf("STEP %d/%s  %s", m.steps, m.stepBudget(), formatElapsed(m.elapsed))}
	runWidth := lipgloss.Width(run.label) + 2
	showRun := leftWidth+runWidth <= width
	available := width - leftWidth - 2
	if showRun {
		available -= runWidth
	}
	if available >= 6 {
		candidates = append(candidates, headerBadge{id: "model", label: strings.TrimSpace(cellLine(modelLabel(m.displayedModel()), available))})
	}
	if showRun {
		candidates = append(candidates, run)
	}
	x := 0
	for i := range candidates {
		if candidates[i].id == "run" {
			x = width - runWidth
		}
		candidates[i].start = x
		candidates[i].end = min(x+lipgloss.Width(candidates[i].label)+2, width)
		x = candidates[i].end
	}
	return candidates
}

func (m *model) badgeLine() string {
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorPopupText)))
	badges := m.headerBadges()
	var out []string
	x := 0
	for _, b := range badges {
		out = append(out, strings.Repeat(" ", max(b.start-x, 0)))
		x = b.end
		style := normal
		switch b.id {
		case "model":
			if !m.usage.liveModel && (m.usage.last.Model != "" || m.usage.historical) {
				style = m.styles.dim
			}
		case "steering":
			style = m.styles.dim
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
		if b.id == "activity" {
			phase, context, _ := strings.Cut(b.label, "  ")
			contextStyle := normal
			if m.usage.historical {
				contextStyle = m.styles.dim
			} else if m.limit > 0 && float64(m.tokens)/float64(m.limit) >= 0.95 {
				contextStyle = m.styles.err
			} else if m.limit > 0 && float64(m.tokens)/float64(m.limit) >= 0.85 {
				contextStyle = m.styles.warn
			}
			out = append(out, style.Render(" "+phase)+m.styles.dim.Render("  ")+contextStyle.Render(context+" "))
			continue
		}
		out = append(out, style.Render(" "+b.label+" "))
	}
	return cellLine(strings.Join(out, ""), m.transcript.Viewport.Width())
}

func (m *model) rawHeaderItems() []menuItem {
	if items := m.menuItems(m.header.menu); items != nil {
		return items
	}
	info := func(s string) menuItem { return menuItem{text: s} }
	idle := m.state == stDone && (m.workflows == nil || !m.workflows.busy)
	switch m.header.menu {

	case "steering":
		return []menuItem{{"Steering", "toggle-steering", true}}
	case "mode":
		return []menuItem{{"ACT   Execute changes with tools", "/mode act", idle}, {"PLAN  Read-only exploration and planning", "/mode plan", idle}}
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
			rows = append(rows, info("Error: "+m.runErr.Error()), menuItem{"Retry failed turn", "/retry", idle && m.retryRun != nil})
		}
		if m.busy() {
			rows = append(rows, menuItem{"Stop run", "stop", m.state == stRunning})
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

func (m *model) contextItems() []menuItem {
	info := func(s string) menuItem { return menuItem{text: s} }
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
	return append(rows, menuItem{"Compact saved history…", "compact-session", idle && m.stateFile != ""})
}

// Wrap informational rows so full model names, endpoints, and errors remain accessible.
func (m *model) headerItems() []menuItem {
	var rows []menuItem
	items := m.rawHeaderItems()
	labelWidth := 0
	for _, item := range items {
		if label, _, ok := strings.Cut(item.text, ": "); ok && item.command == "" {
			labelWidth = max(labelWidth, lipgloss.Width(label+":"))
		}
	}
	for _, item := range items {
		if label, value, ok := strings.Cut(item.text, ": "); ok && item.command == "" {
			item.text = cellLine(label+":", labelWidth) + "  " + value
		}
		if item.command != "" {
			rows = append(rows, item)
			continue
		}
		for _, text := range strings.Split(ansi.Wrap(item.text, max(min(58, m.termW)-4, 1), ""), "\n") {
			rows = append(rows, menuItem{text: text})
		}
	}
	return rows
}

func (m *model) closeHeader() { m.header.focused = false; m.header.menu = ""; m.header.global = false }
func (m *model) headerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.normalizeHeader()
	switch msg.String() {
	case "ctrl+q", "f10":
		return m.requestQuit()
	case "ctrl+c":
		m.quit = true
		m.cancel()
		return m, tea.Quit
	case "esc":
		m.closeHeader()
		return m, nil
	case "f9":
		m.closeHeader()
		return m, nil
	case "left", "right":
		delta := 1
		if msg.String() == "left" {
			delta = -1
		}
		badges := m.headerNavigation()
		m.header.index = (m.header.index + delta + len(badges)) % len(badges)
		if m.header.menu != "" {
			m.header.menu = badges[m.header.index].id
			m.header.row = m.headerActionRow(0, 1)
		}
	case "up", "down":
		delta := 1
		if msg.String() == "up" {
			delta = -1
		}
		if m.header.global {
			m.header.row = m.headerActionRow(m.header.row+delta, delta)
		} else {
			m.header.row = min(max(m.header.row+delta, 0), max(len(m.headerItems())-1, 0))
		}
	case "enter":
		if m.header.menu == "" {
			m.header.menu = m.headerNavigation()[min(m.header.index, len(m.headerNavigation())-1)].id
			m.header.row = m.headerActionRow(0, 1)
			return m, nil
		}
		items := m.headerItems()
		if m.header.row >= len(items) {
			return m, nil
		}
		item := items[m.header.row]
		if item.enabled && item.command != "" {

			return m.activateMenuAction(item.command)
		}
	}
	return m, nil
}

func (m *model) headerMenuItems() []popup.MenuItem {
	var items []popup.MenuItem
	for _, item := range m.headerItems() {
		_, checked := m.menuToggle(item.command)
		items = append(items, popup.MenuItem{Label: item.text, Shortcut: menuShortcut(item.command),
			Enabled: item.enabled && item.command != "", Checked: checked,
			Separator: item.text == "" && item.command == "", Info: item.text != "" && item.command == ""})
	}
	return items
}

func (m *model) headerMenuLayout() popup.MenuLayout {
	x := 0
	for _, b := range m.headerNavigation() {
		if b.id == m.header.menu || (b.id == "overflow" && m.header.menu != "session-info") {
			x = b.start
			if !m.header.global {
				x += m.promptOffset()
			}
			break
		}
	}
	normal, border, selected := popupStyles()
	return popup.Menu(m.headerMenuItems(), popup.MenuProps{X: x, Y: m.headerTop(), ScreenWidth: m.termW,
		ScreenHeight: m.termH - m.actionBarRows(), MinWidth: 24, MaxWidth: 58,
		MaxRows: max(m.termH-m.footerRows()-5, 1), Selected: m.header.row},
		popup.MenuStyles{Surface: popup.Styles{Normal: normal, Border: border, Selected: selected},
			Disabled: normal.Foreground(lipgloss.Color(themeColor(colorDisabledText)))})
}

func (m *model) headerGeometry() (x, width, capacity, offset int) {
	l := m.headerMenuLayout()
	return l.X, l.Width, l.Capacity, l.Offset
}

func (m *model) overlayHeader(background string) string {
	l := m.headerMenuLayout()
	if len(l.Rows) == 0 {
		return background
	}
	return popup.Overlay(background, l.Rows, l.X, l.Y, l.Width, m.termW)
}

func (m *model) headerMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Button == tea.MouseLeft && isMouseClick(msg) && mouse.Y == 0 {
		for i, cell := range m.headerNavigationForMenus() {
			if mouse.X >= cell.start && mouse.X < cell.end {
				if m.dialog != nil || m.sessions != nil || m.state == stAsk || m.state == stPermission {
					return true, nil
				}
				m.header = headerState{focused: true, global: true, index: i, menu: cell.id}
				m.header.row = m.headerActionRow(0, 1)
				return true, nil
			}
		}
	}

	if m.dialog != nil || m.sessions != nil || m.state == stAsk || m.state == stPermission {
		return false, nil
	}
	if m.actionBarVisible() && mouse.Y >= m.termH-m.actionBarRows() {
		return false, nil
	}

	if m.header.menu != "" {
		layout := m.headerMenuLayout()
		if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
			if row := layout.Hit(mouse.X, mouse.Y); row >= 0 {
				m.header.row = row
				_, cmd := m.headerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
				return true, cmd
			}
			if !layout.Contains(mouse.X, mouse.Y) {
				m.closeHeader()
			}
		}
		if layout.Contains(mouse.X, mouse.Y) {
			if mouse.Button == tea.MouseWheelUp {
				m.header.row = m.headerActionRow(m.header.row-1, -1)
			}
			if mouse.Button == tea.MouseWheelDown {
				m.header.row = m.headerActionRow(m.header.row+1, 1)
			}
		}
		return true, nil
	}

	if m.header.focused && mouse.Button == tea.MouseLeft && isMouseClick(msg) {
		m.closeHeader()
		return true, nil
	}
	return false, nil
}

func (m *model) normalizeHeader() {
	m.header.index = min(max(m.header.index, 0), len(m.headerNavigation())-1)
	m.header.row = min(max(m.header.row, 0), max(len(m.headerItems())-1, 0))
}

func (m *model) menuCells() []footerCell {
	enabled := m.dialog == nil && m.sessions == nil && m.state != stPermission && m.state != stAsk
	x := 1
	var cells []footerCell
	menus := m.visibleMenus()
	for _, menu := range menus {
		if x >= m.termW-1 {
			break
		}
		end := min(x+lipgloss.Width(menu.label)+2, m.termW-1)
		cells = append(cells, footerCell{action: footerAction{name: menu.id, label: menu.label, enabled: enabled}, start: x, end: end})
		x = end + 1
	}
	return cells
}
func (m *model) headerNavigation() []headerBadge {
	if !m.header.global {
		return m.headerBadges()
	}
	return m.headerNavigationForMenus()
}

func (m *model) headerNavigationForMenus() []headerBadge {
	var badges []headerBadge
	for _, cell := range m.menuCells() {
		badges = append(badges, headerBadge{id: cell.action.name, label: cell.action.label, start: cell.start, end: cell.end})
	}
	if b := m.sessionIdentityBadge(); b.id != "" {
		badges = append(badges, b)
	}
	return badges
}
func (m *model) menuLine() string {
	normal, _, selected := popupStyles()
	line := ""
	for i, cell := range m.menuCells() {
		line += normal.Render(strings.Repeat(" ", max(cell.start-lipgloss.Width(line), 0)))
		label := cellLine(" "+cell.action.label, cell.end-cell.start)
		if !cell.action.enabled {
			label = normal.Foreground(lipgloss.Color(themeColor(colorDisabledText))).Render(label)
		} else if m.header.focused && m.header.global && m.header.index == i {
			label = selected.Render(label)
		} else {
			label = normal.Render(label)
		}
		line += label
	}
	if b := m.sessionIdentityBadge(); b.label != "" {
		line += normal.Render(strings.Repeat(" ", max(b.start-lipgloss.Width(line), 0)))
		style := normal.Foreground(lipgloss.Color(themeColor(colorSecondaryText)))
		if b.id != "" {
			style = style.Underline(true)
			if m.header.focused && m.header.global && m.headerNavigation()[min(m.header.index, len(m.headerNavigation())-1)].id == b.id {
				style = selected
			}
		}
		line += style.Render(b.label)
	}
	return line + normal.Render(strings.Repeat(" ", max(m.termW-lipgloss.Width(line), 0)))
}

// sessionIdentityBadge shares visible geometry between rendering and mouse hits.
func (m *model) sessionIdentityBadge() headerBadge {
	used := 0
	for _, cell := range m.menuCells() {
		used = cell.end
	}
	available := m.termW - used - 3
	if available < 12 {
		return headerBadge{}
	}
	identity := m.activeSessionTitle()
	id := ""
	if m.stateFile != "" {
		identity = filepath.Base(filepath.Dir(m.stateFile)) + " · " + identity
		id = "session-info"
	}
	if m.ws != nil {
		identity = filepath.Base(m.ws.Root()) + " · " + identity
	}
	if m.workflowActive() {
		identity, id = filepath.Base(m.wsRoot), ""
	}
	identity = strings.TrimSpace(cellLine(identity, available))
	end := m.termW - 1
	return headerBadge{id: id, label: identity, start: end - lipgloss.Width(identity), end: end}
}

func (m *model) headerTop() int {
	if m.header.global {
		return 1
	}
	return 3
}

func (m *model) headerActionRow(start, delta int) int {
	items := m.headerItems()
	if len(items) == 0 {
		return 0
	}
	if !m.header.global {
		return min(max(start, 0), len(items)-1)
	}
	return popup.MenuNext(m.headerMenuItems(), start, delta)
}
