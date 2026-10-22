package tui

import (
	"fmt"
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
type headerItem struct {
	text, command string
	enabled       bool
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
			if m.progress.PromptTokens > 0 {
				phase += fmt.Sprintf(" %d%%", min(100, m.progress.ProcessedTokens*100/m.progress.PromptTokens))
			}
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
	ctx := "CTX " + kTokens(m.tokens)
	if m.tokensEst {
		ctx = "~" + ctx
	}
	if m.limit > 0 {
		ctx += "/" + kTokens(m.limit)
	}
	modelWidth := 32
	if m.transcript.Viewport.Width() < 110 {
		modelWidth = 18
	}
	candidates := []headerBadge{{id: "mode", label: mode}, {id: "activity", label: activity}, {id: "model", label: strings.TrimSpace(cellLine(modelLabel(m.displayedModel()), modelWidth))}, {id: "context", label: ctx}}
	label := fmt.Sprintf("step %d/%s  %s", m.steps, m.stepBudget(), formatElapsed(m.elapsed))
	candidates = append(candidates, headerBadge{id: "run", label: label})
	// Context and state stay visible before the optional model and run details.
	total := 0
	for _, b := range candidates {
		total += lipgloss.Width(b.label) + 2
	}
	total += lipgloss.Width(m.badgePaneLabel())
	if total > m.transcript.Viewport.Width() {
		available := lipgloss.Width(candidates[2].label) - (total - m.transcript.Viewport.Width())
		if available >= 6 {
			candidates[2].label = strings.TrimSpace(cellLine(candidates[2].label, available))
		} else {
			candidates = append(candidates[:2], candidates[3:]...)
		}
	}
	x := lipgloss.Width(m.badgePaneLabel())
	for i := range candidates {
		candidates[i].start = x
		candidates[i].end = min(x+lipgloss.Width(candidates[i].label)+2, m.transcript.Viewport.Width())
		x = candidates[i].end
	}
	return candidates
}

func (m *model) badgeLine() string {
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(colorPopupText)))
	_, _, selected := popupStyles()
	badges := m.headerBadges()
	var out []string
	for _, b := range badges {
		style := normal
		switch b.id {
		case "model":
			if !m.usage.liveModel && (m.usage.last.Model != "" || m.usage.historical) {
				style = m.styles.dim
			}
		case "context":
			if m.usage.historical {
				style = m.styles.dim
			}
		case "mode":
			style = style.Foreground(lipgloss.Color(themeColor(colorMode)))
		case "activity":
			color := colorReady
			if m.state != stDone {
				color = colorBusy
			}
			if m.runErr != nil {
				color = colorFailed
			}
			style = style.Foreground(lipgloss.Color(themeColor(color)))

		}
		out = append(out, style.Render(" "+b.label+" "))
	}
	label := m.badgePaneLabel()
	titleStyle := m.styles.dim
	if !m.navFocused {
		titleStyle = selected
	}
	return cellLine(titleStyle.Render(label)+strings.Join(out, ""), m.transcript.Viewport.Width())
}

func (m *model) badgePaneLabel() string {
	return " HISTORY "
}

func (m *model) rawHeaderItems() []headerItem {
	info := func(s string) headerItem { return headerItem{text: s} }
	idle := m.state == stDone
	switch m.header.menu {
	case "session":
		rows := []headerItem{{"New", "/new", idle}, {"Open…", "/sessions", true}, {"Compact history", "/compact", idle && m.stateFile != ""}, {"Rename…", "rename-current", idle && m.stateFile != ""}, {"Delete…", "delete-current", idle && m.stateFile != ""}}

		return append(rows, headerItem{"", "", false}, headerItem{"Quit", "quit", true})
	case "workflow":
		return []headerItem{{"Plan", "/mode plan", idle}, {"Act", "/mode act", idle}, {"Stop run", "stop", m.state == stRunning}, {"Retry", "/retry", idle && m.runErr != nil && m.retryRun != nil}, {"Permissions…", "permissions-info", true}}
	case "view":
		// Two zones: what the reader has chosen to hide, then what they
		// can do. The separator exists because a filtered transcript and
		// a scroll action are not the same kind of thing, and listing
		// them as one flat list hid that.
		return []headerItem{
			{"Hide Sessions", "toggle-sidebar", m.termW >= navigatorMinWidth},
			{"Truncate AI reply", "toggle-details", true},
			{"Hide SYS messages", "toggle-sys", true},
			{"Hide labels", "toggle-labels", true},
			{"", "", false},
			{"Jump to latest", "latest", true},
			{"Select Theme…", "theme", true},
		}
	case "settings":
		editable := idle && m.config != nil
		rows := []headerItem{{"Model…", "edit:model", editable}, {"Generation…", "edit:generation", editable}, {"Profiles…", "edit:profiles", editable}, {"Application…", "application", idle && m.application != nil}}
		if m.config != nil {
			rows = append(rows, headerItem{"You…", "edit:you", editable}, headerItem{"Prompts…", "edit:prompts", editable}, headerItem{"Prompt preview…", "prompt-info", true})
		}
		return rows
	case "help":
		return []headerItem{{"Keyboard shortcuts", "help-keys", true}, {"Commands", "help-commands", true}, {"About", "about", true}}

	case "mode":
		return []headerItem{{"ACT   Execute changes with tools", "/mode act", idle}, {"PLAN  Read-only exploration and planning", "/mode plan", idle}}
	case "activity":
		rows := []headerItem{info("State: " + m.statusWord()), info("Elapsed: " + formatElapsed(m.elapsed)), info(m.stepLine())}
		if m.progress.PromptTokens > 0 {
			rows = append(rows, info(fmt.Sprintf("Input: %s   Generated: %s", kTokens(m.progress.PromptTokens), kTokens(m.progress.CompletionTokens))))
			if m.progress.CachedTokens > 0 {
				rows = append(rows, info("Cached input: "+kTokens(m.progress.CachedTokens)))
			}
		}
		if m.runErr != nil {
			rows = append(rows, info("Error: "+m.runErr.Error()), headerItem{"Retry failed turn", "/retry", idle && m.retryRun != nil})
		}
		if m.busy() {
			rows = append(rows, headerItem{"Stop run", "stop", m.state == stRunning})
		}
		return rows
	case "model":
		transport := "Request / response"
		if m.streaming {
			transport = "Streaming"
		}
		rows := []headerItem{info("Model: " + nonEmpty(m.displayedModel())), info("Backend: " + nonEmpty(m.backendKind)), info("Endpoint: " + nonEmpty(m.backendURL)), info("Transport: " + transport), info(fmt.Sprintf("Context window: %d tokens", m.limit)), info(fmt.Sprintf("Response budget: %d tokens", m.responseCap))}
		if !m.usage.liveModel && m.usage.historical {
			rows = append(rows, info("Current backend model: "+nonEmpty(m.modelName)))
		}
		return rows
	case "context":
		rows := []headerItem{info("Context: " + strings.Replace(m.meter(), "ctx ", "", 1)), info("Source: " + nonEmpty(m.usage.source))}
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
		return append(rows, headerItem{"Compact saved history", "/compact", idle && m.stateFile != ""})
	case "run":
		rows := []headerItem{info(m.stepLine()), info("Elapsed: " + formatElapsed(m.elapsed)), info(fmt.Sprintf("Tools: %d   Files: %d", m.toolsUsed, len(m.filesTouched)))}
		if !m.firstToken.IsZero() {
			rows = append(rows, info("First token: "+formatElapsed(m.firstToken.Sub(m.started))))
		}
		return rows

	}
	return nil
}

// Wrap informational rows so full model names, endpoints, and errors remain accessible.
func (m *model) headerItems() []headerItem {
	var rows []headerItem
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
			rows = append(rows, headerItem{text: text})
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

			m.closeHeader()
			switch item.command {
			case "permissions-info":
				lines := []string{"Plan exposes read-only tools. Act uses the configured policy and approval prompts.", "Settings changes do not approve tool execution.", "", "Policy rules (later rules take precedence)"}
				lines = append(lines, m.policyDetails...)
				m.openDialog("Permissions", lines)
				return m, nil
			case "help-keys", "help-commands":
				m.openHelp()
				m.dialog.title = "Keyboard shortcuts"
				if item.command == "help-commands" {
					m.dialog.title = "Commands"
				}
				return m, nil
			case "theme":
				m.openTheme()
				return m, nil
			case "about":
				return m.command("/about")
			case "application":
				m.openApplication()
				return m, nil
			case "prompt-info":
				m.inspectPrompt()
				return m, nil
			case "rename-current":
				m.renameActive()
				return m, nil
			case "delete-current":
				m.deleteActive()
				return m, nil
			case "connection-info":
				m.inspectConnection()
				return m, nil
			case "context-info":
				m.openDialog("Context usage", []string{m.meter(), "Measurement source: " + nonEmpty(m.usage.source), "/compact is available for coding task history."})
				return m, nil
			case "quit":
				return m.requestQuit()
			case "toggle-sidebar":
				return m.handleKey(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
			case "toggle-details":
				return m.handleKey(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
			case "toggle-sys":
				m.hideSYS = !m.hideSYS
				m.refreshContent()
				return m, nil
			case "toggle-labels":
				m.hideLabels = !m.hideLabels
				m.refreshContent()
				return m, nil
			case "latest":
				return m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
			}
			if strings.HasPrefix(item.command, "edit:") {
				m.editSettings(strings.TrimPrefix(item.command, "edit:"))
				return m, nil
			}
			if item.command == "stop" {
				m.interrupted = true
				m.cancel()
				m.state = stStopping
				m.fitBottom()
				return m, nil
			}
			return m.command(item.command)
		}
	}
	return m, nil
}

func (m *model) headerGeometry() (x, width, capacity, offset int) {
	width = 24
	for _, item := range m.rawHeaderItems() {
		width = max(width, lipgloss.Width(item.text)+lipgloss.Width(menuShortcut(item.command))+7)
	}
	width = min(width, min(58, m.termW))
	x = 0
	for _, b := range m.headerNavigation() {
		if b.id == m.header.menu {
			x = b.start
			if !m.header.global {
				x += m.promptOffset()
			}
			break
		}
	}
	x = min(x, max(m.termW-width, 0))
	capacity = max(min(len(m.headerItems()), m.termH-m.footerRows()-5), 1)
	row := min(max(m.header.row, 0), max(len(m.headerItems())-1, 0))
	offset = max(row-capacity+1, 0)
	return
}
func (m *model) overlayHeader(background string) string {
	x, width, capacity, offset := m.headerGeometry()
	if width < 4 || m.termH < 6 {
		return background
	}
	normal, border, selected := popupStyles()
	rows := []string{border.Render("╭" + strings.Repeat("─", width-2) + "╮")}
	items := m.headerItems()
	for i := offset; i < min(offset+capacity, len(items)); i++ {
		style := normal
		prefix := "  "
		if items[i].command != "" {
			prefix = "  "
			if !items[i].enabled {
				style = border
			}
		}
		if i == m.header.row {
			style = selected
		}
		if items[i].text == "" && items[i].command == "" {
			rows = append(rows, border.Render("├"+strings.Repeat("─", width-2)+"┤"))
			continue
		}
		// The tick lives in the label indent every row already carries,
		// not in a column of its own. "✓ " and "  " are both two cells, so
		// a toggle marks itself in space that was already reserved and no
		// row grows by two cells for it.
		if _, on := m.headerToggle(items[i].command); on {
			prefix = "✓ "
		}
		label := prefix + items[i].text
		shortcut := menuShortcut(items[i].command)
		if shortcut != "" {
			label += strings.Repeat(" ", max(1, width-2-lipgloss.Width(label)-lipgloss.Width(shortcut))) + shortcut
		}
		rows = append(rows, border.Render("│")+popupRow(style, label, width-2)+border.Render("│"))
	}
	rows = append(rows, border.Render("╰"+strings.Repeat("─", width-2)+"╯"))
	return popup.Overlay(background, rows, x, m.headerTop(), width, m.termW)
}

func (m *model) headerMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Button == tea.MouseLeft && isMouseClick(msg) && mouse.Y == 0 {
		for i, cell := range m.menuCells() {
			if mouse.X >= cell.start && mouse.X < cell.end {
				if !cell.action.enabled {
					return true, nil
				}
				m.header = headerState{focused: true, global: true, index: i, menu: cell.action.name}
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
		x, w, capacity, offset := m.headerGeometry()
		if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
			if mouse.X >= x && mouse.X < x+w && mouse.Y >= m.headerTop()+1 && mouse.Y < m.headerTop()+1+capacity {
				m.header.row = offset + mouse.Y - m.headerTop() - 1
				_, cmd := m.headerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
				return true, cmd
			} else {
				m.closeHeader()
			}
		}
		if mouse.Button == tea.MouseWheelUp {
			m.header.row = max(m.header.row-1, 0)
		}
		if mouse.Button == tea.MouseWheelDown {
			m.header.row = min(m.header.row+1, len(m.headerItems())-1)
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
	menus := []struct{ id, label string }{{"session", "Session"}, {"workflow", "Run"}, {"view", "View"}, {"settings", "Settings"}, {"help", "Help"}}
	for _, menu := range menus {
		if x >= m.termW-1 {
			break
		}
		if menu.id == "workflow" {
			if m.application != nil {
				menu.label = "Agent"
			}
		}
		end := min(x+len(menu.label)+2, m.termW-1)
		cells = append(cells, footerCell{action: footerAction{name: menu.id, label: menu.label, enabled: enabled}, start: x, end: end})
		x = end + 1
	}
	return cells
}
func (m *model) headerNavigation() []headerBadge {
	if !m.header.global {
		return m.headerBadges()
	}
	var badges []headerBadge
	for _, cell := range m.menuCells() {
		badges = append(badges, headerBadge{id: cell.action.name, label: cell.action.label, start: cell.start, end: cell.end})
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
	return line + normal.Render(strings.Repeat(" ", max(m.termW-lipgloss.Width(line), 0)))
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
	for i := 0; i < len(items); i++ {
		row := ((start+i*delta)%len(items) + len(items)) % len(items)
		if items[row].enabled && items[row].command != "" {
			return row
		}
	}
	return 0
}

// headerToggle reports whether a menu row is a checkable toggle, and
// whether the state its label names is currently on.
//
// The checkmark sits in the label indent every menu row already carries,
// so it replaces an idle cell rather than adding one. A separate column
// cost two cells of the popup's width on every row to hold a mark that
// is blank on most of them.
//
// on is the state the label names, not the state a toggle flips to: the
// mark on "Hide Sessions" means the sidebar is hidden.
func (m *model) headerToggle(command string) (toggle, on bool) {
	switch command {
	case "toggle-sidebar":
		return true, m.sidebarHidden
	case "toggle-details":
		return true, m.transcript.compact
	case "toggle-sys":
		return true, m.hideSYS
	case "toggle-labels":
		return true, m.hideLabels
	case "/mode plan":
		return true, m.plan
	case "/mode act":
		return true, !m.plan
	}
	return false, false
}

// menuShortcut names the key that reaches a menu row without opening
// the menu. A row with no shortcut still works by click.
func menuShortcut(command string) string {
	switch command {
	case "/new":
		return "Ctrl+N"
	case "quit":
		return "Ctrl+Q"
	case "toggle-sidebar":
		return "Ctrl+B"
	case "toggle-details":
		return "Ctrl+G"
	case "latest":
		return "Ctrl+End"
	case "stop":
		return "Esc"
	case "/help", "help-keys":
		return "F1"
	}
	return ""
}
