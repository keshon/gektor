package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

const inspectorSplitWidth = 100

type toolInspector struct {
	drag               scrollDrag
	callID             string
	focused, selecting bool
	selected           int
	pane               transcriptState
	children           map[string]*childTranscript
}

func (m *Model) inspectorVisible() bool {
	return m.inspector.callID != "" && m.dialog == nil && m.sessions == nil && m.state != stPermission && m.state != stAsk
}
func (m *Model) mainWidth() int {
	return m.screenPanes(m.inspectorVisible()).Main.Width
}
func (m *Model) inspectorBlock() *block {
	for i := len(m.transcript.blocks) - 1; i >= 0; i-- {
		b := &m.transcript.blocks[i]
		if b.role == roleTool && b.callID == m.inspector.callID {
			return b
		}
	}
	return nil
}
func (m *Model) openInspector(index int) tea.Cmd {
	if index < 0 || index >= len(m.transcript.blocks) {
		return nil
	}
	b := m.transcript.blocks[index]
	if b.role != roleTool || b.callID == "" {
		return nil
	}
	if m.inspector.callID != b.callID {
		m.inspector.drag = scrollDrag{}
		m.inspector.pane = newTranscript()
		m.inspector.pane.compact = false
	}
	m.transcript.Follow = false
	m.inspector.callID = b.callID
	m.inspector.selected = index
	m.inspector.selecting = false
	m.inspector.focused = true
	m.fitColumns()
	m.fitBottom()
	m.refreshContent()
	m.refreshInspector()
	if !strings.HasPrefix(b.text, "delegate_task ") {
		return nil
	}
	child := m.child(b.callID)
	if child.loaded {
		return nil
	}
	parent, id, revision := m.stateFile, b.callID, child.revision
	path := conversation.ChildStatePath(parent, id)
	if path == "" {
		child.notice = "No saved child history for this call."
		m.refreshInspector()
		return nil
	}
	return func() tea.Msg {
		snapshot, err := conversation.Load(path)
		return childLoadedMsg{parent: parent, callID: id, revision: revision, snapshot: snapshot, err: err}
	}
}
func (m *Model) closeInspector() {
	m.inspector.drag = scrollDrag{}
	m.inspector.callID = ""
	m.inspector.focused = false
	m.inspector.selecting = false
	m.fitColumns()
	m.fitBottom()
	m.refreshContent()
}
func (m *Model) refreshInspector() {
	b := m.inspectorBlock()
	if b == nil {
		return
	}
	width := m.screenPanes(true).Side.Width
	layout := m.panelGeometry(width, 1, true, false)
	pane := &m.inspector.pane
	pane.Viewport.SetWidth(max(layout.Body.Width, 1))
	pane.Viewport.SetHeight(max(layout.Body.Height, 1))
	pane.compact = false
	pane.blocks = []block{resultBlock("Arguments\n" + prettyToolArguments(b.text))}
	live, thinking := "", ""
	if strings.HasPrefix(b.text, "delegate_task ") {
		child := m.child(b.callID)
		blocks, _ := renderHistoryAt(child.history)
		for i := range blocks {
			if blocks[i].role == roleTool && blocks[i].open {
				blocks[i].result = child.output[blocks[i].callID]
			}
		}
		pane.blocks = append(pane.blocks, inspectionBlocks(blocks)...)
		if len(child.history) == 0 && b.result != "" {
			pane.blocks = append(pane.blocks, resultBlock("Returned result\n"+b.result))
		}
		if child.notice != "" {
			pane.blocks = append(pane.blocks, markerBlock(child.notice))
		}
		live, thinking = child.live, child.thinking
	} else if b.result != "" {
		pane.blocks = append(pane.blocks, resultBlock("Output\n"+b.result))
	}
	state := "Running"
	if b.returned {
		state = "Returned · awaiting result processing"
	}
	if b.open && strings.HasPrefix(b.text, "delegate_task ") && m.child(b.callID).phase != "" {
		state += " · " + m.child(b.callID).phase
	}
	if !b.open {
		state = "Completed"
		if b.toolStatus == "skipped_for_input" {
			state = "Skipped for input"
		}
		if b.failed {
			state = "Failed"
		}
	}
	pane.blocks = append(pane.blocks, markerBlock(state))
	for i := range pane.blocks {
		pane.blocks[i].at = time.Time{}
	}
	pane.refresh(transcriptProps{speaker: "Subagent", user: m.userName(), styles: m.styles, live: live, liveThink: thinking, hideNames: true, labels: false})
}
func prettyToolArguments(text string) string {
	name, args, ok := strings.Cut(text, " ")
	if !ok {
		return text
	}
	var raw bytes.Buffer
	if err := json.Indent(&raw, []byte(args), "", "  "); err != nil {
		return text
	}
	return name + "\n" + raw.String()
}
func inspectionBlocks(blocks []block) []block {
	out := make([]block, 0, len(blocks))
	for _, b := range blocks {
		if b.role == roleTool {
			out = append(out, resultBlock("Tool call\n"+prettyToolArguments(b.text)))
			if b.result != "" {
				out = append(out, resultBlock("Output\n"+b.result))
			}
		} else {
			switch b.role {
			case roleUser:
				b.text = "Task\n" + b.text
			case roleThink:
				b.text = "Reasoning\n" + b.text
			case roleAnswer:
				b.text = "Response\n" + b.text
			}
			out = append(out, b)
		}
	}
	return out
}
func (m *Model) composeInspector(content string) string {
	if !m.inspectorVisible() {
		return content
	}
	b := m.inspectorBlock()
	if b == nil {
		return content
	}
	name, _, _ := strings.Cut(b.text, " ")
	regions := m.screenPanes(true)
	width, left := regions.Side.Width, regions.Side.X
	heading := "Inspect / " + name
	if m.inspector.focused {
		heading = "Inspect / " + name + " [focused]"
	}
	shortcuts := []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "Tab", Action: "parent"}, {Key: "↑↓", Action: "scroll"}}
	if left == 0 {
		shortcuts = []popup.Shortcut{{Key: "Esc", Action: "return to conversation"}, {Key: "↑↓", Action: "scroll"}}
	}
	hint := m.inspectionHints(width, shortcuts...)
	rows := m.inspectionRows(heading, hint, m.inspector.pane.View(), width, m.inspector.focused, true)
	base := strings.Split(content, "\n")
	for i, row := range rows {
		y := i + regions.Side.Y
		if y >= len(base) {
			break
		}
		if left == 0 {
			base[y] = cellLine(row, width)
		} else {
			base[y] = cellLine(ansi.Truncate(base[y], left, ""), left) + cellLine(row, width)
		}
	}
	return strings.Join(base, "\n")
}

// inspectionRows uses the same frame and one-row footer for both inspectors.
func (m *Model) inspectionRows(heading, hint, body string, width int, focused, close bool) []string {
	layout := m.panelGeometry(width, 1, close, false)
	return pane.Render(layout, heading, body, []string{hint}, m.paneStyles(), focused && !m.header.focused)
}

func (m *Model) inspectionHints(width int, shortcuts ...popup.Shortcut) string {
	inner := m.panelGeometry(width, 1, true, false).Footer.Width
	return popup.Shortcuts(inner, m.shortcutStyles(), shortcuts...)
}

func (m *Model) inspectorKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.dialog != nil || m.sessions != nil || m.header.menu != "" || m.header.focused || m.state == stPermission || m.state == stAsk {
		return false, nil
	}
	key := msg.String()
	if key == "ctrl+c" || key == "ctrl+q" || (msg.Code >= tea.KeyF1 && msg.Code <= tea.KeyF10) {
		return false, nil
	}
	if key == "ctrl+t" {
		m.inspector.selecting = true
		m.inspector.focused = false
		m.selectTool(0)
		return true, nil
	}
	if m.inspector.selecting {
		switch key {
		case "esc", "tab":
			m.inspector.selecting = false
			m.refreshContent()
		case "up":
			m.selectTool(-1)
		case "down":
			m.selectTool(1)
		case "enter":
			return true, m.openInspector(m.inspector.selected)
		}
		return true, nil
	}
	if !m.inspectorVisible() {
		return false, nil
	}
	if key == "tab" && m.termW >= inspectorSplitWidth {
		m.inspector.focused = !m.inspector.focused
		return true, nil
	}
	if !m.inspector.focused && m.termW >= inspectorSplitWidth {
		return false, nil
	}
	switch key {
	case "esc":
		m.closeInspector()
		return true, nil
	case "ctrl+c", "ctrl+q":
		return false, nil
	case "up", "down", "home", "end":
		v := &m.inspector.pane.Viewport
		switch key {
		case "up":
			v.SetYOffset(v.YOffset() - 1)
		case "down":
			v.SetYOffset(v.YOffset() + 1)
		case "home":
			v.GotoTop()
		case "end":
			v.GotoBottom()
		}
		m.inspector.pane.Follow = v.AtBottom()
		return true, nil
	case "pgup", "pgdown":
		return true, m.inspector.pane.Update(msg)
	}
	return true, nil
}
func (m *Model) selectTool(direction int) {
	indices := []int{}
	for i, b := range m.transcript.blocks {
		if b.role == roleTool && b.callID != "" {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		m.inspector.selecting = false
		return
	}
	position := len(indices) - 1
	if direction != 0 {
		for j, index := range indices {
			if index == m.inspector.selected {
				position = j
				break
			}
		}
		position = max(0, min(position+direction, len(indices)-1))
	}
	m.inspector.selected = indices[position]
	m.refreshContent()
	for part, index := range m.transcript.render.partBlocks {
		if index == m.inspector.selected {
			m.transcript.Viewport.SetYOffset(m.transcript.PartOffset(part))
			m.transcript.Follow = false
			break
		}
	}
}
func (m *Model) inspectorScrollbarMouse(msg tea.MouseMsg) bool {
	area := m.screenPanes(true).Side
	layout := m.panelGeometry(area.Width, 1, true, false)
	return viewportScrollbarMouse(&m.inspector.pane.Viewport, &m.inspector.pane.Follow, &m.inspector.drag,
		area.X+layout.Body.X+layout.Body.Width-1, area.Y+layout.Body.Y, msg)
}

func (m *Model) inspectorMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	mouse := msg.Mouse()
	area := m.screenPanes(true).Side
	layout := m.panelGeometry(area.Width, 1, true, false)
	if m.inspectorVisible() && area.Contains(mouse.X, mouse.Y) {
		if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
			if layout.CloseHit.Contains(mouse.X-area.X, mouse.Y-area.Y) {
				m.closeInspector()
				return true, nil
			}
			m.inspector.focused = true
		}
		if m.inspectorScrollbarMouse(msg) {
			return true, nil
		}
		if mouse.Button == tea.MouseWheelUp || mouse.Button == tea.MouseWheelDown {
			if layout.Body.Contains(mouse.X-area.X, mouse.Y-area.Y) {
				return true, m.inspector.pane.Update(msg)
			}
		}
		return true, nil
	}
	if mouse.Button != tea.MouseLeft || !isMouseClick(msg) {
		return false, nil
	}
	m.inspector.focused = false
	m.inspector.selecting = false
	if mouse.Y < paneTop || mouse.Y >= paneTop+m.transcript.Viewport.Height() {
		return false, nil
	}
	row := mouse.Y - paneTop + m.transcript.Viewport.YOffset()
	for part, index := range m.transcript.render.partBlocks {
		hit, ok := m.transcript.render.toolHits[part]
		x := mouse.X - m.promptOffset()
		if ok && row == m.transcript.PartOffset(part)+hit.row && x >= hit.start && x < hit.end && mouse.X != m.scrollbarX() {
			return true, m.openInspector(index)
		}
	}
	return false, nil
}

// inspectorHint is outside the transcript, so keyboard selection is visible
// even when the selected tool's result occupies more than one screen.
func (m *Model) inspectorHint() string {
	if m.inspector.selecting && m.inspector.selected >= 0 && m.inspector.selected < len(m.transcript.blocks) {
		name, _, _ := strings.Cut(m.transcript.blocks[m.inspector.selected].text, " ")
		return m.controlHints(popup.Shortcut{Key: "Esc", Action: "composer"}, popup.Shortcut{Key: "Enter", Action: "inspect"}, popup.Shortcut{Key: "↑↓", Action: "tools"}) + "  Selected " + name
	}
	return ""
}
