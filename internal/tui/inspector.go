package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/session"
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

func (m *model) inspectorVisible() bool {
	return m.inspector.callID != "" && m.dialog == nil && m.sessions == nil && m.state != stPermission && m.state != stAsk
}
func (m *model) mainWidth() int {
	if m.inspectorVisible() && m.termW >= inspectorSplitWidth {
		return m.termW / 2
	}
	return m.termW
}
func (m *model) inspectorBlock() *block {
	for i := len(m.transcript.blocks) - 1; i >= 0; i-- {
		b := &m.transcript.blocks[i]
		if b.role == roleTool && b.callID == m.inspector.callID {
			return b
		}
	}
	return nil
}
func (m *model) openInspector(index int) tea.Cmd {
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
	path := session.ChildStatePath(parent, id)
	if path == "" {
		child.notice = "No saved child history for this call."
		m.refreshInspector()
		return nil
	}
	return func() tea.Msg {
		snapshot, err := session.Load(path)
		return childLoadedMsg{parent: parent, callID: id, revision: revision, snapshot: snapshot, err: err}
	}
}
func (m *model) closeInspector() {
	m.inspector.drag = scrollDrag{}
	m.inspector.callID = ""
	m.inspector.focused = false
	m.inspector.selecting = false
	m.fitColumns()
	m.fitBottom()
	m.refreshContent()
}
func (m *model) refreshInspector() {
	b := m.inspectorBlock()
	if b == nil {
		return
	}
	width := m.termW
	if m.termW >= inspectorSplitWidth {
		width = m.termW - m.termW/2
	}
	pane := &m.inspector.pane
	pane.Viewport.SetWidth(max(width-2*m.promptOffset(), 1))
	pane.Viewport.SetHeight(max(m.termH-m.footerRows()-5, 1))
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
func (m *model) composeInspector(content string) string {
	if !m.inspectorVisible() {
		return content
	}
	b := m.inspectorBlock()
	if b == nil {
		return content
	}
	name, _, _ := strings.Cut(b.text, " ")
	width := m.termW
	left := 0
	if m.termW >= inspectorSplitWidth {
		left = m.mainWidth()
		width = m.termW - left
	}
	heading := "Inspect · " + name
	if m.inspector.focused {
		heading = "Inspect · " + name + " [focused]"
	}
	hint := "Esc close · Tab parent · ↑↓ scroll"
	if left == 0 {
		hint = "Esc return to conversation · ↑↓ scroll"
	}
	rows := m.inspectionRows(heading, hint, m.inspector.pane.View(), width, m.inspector.pane.Viewport.Height())
	base := strings.Split(content, "\n")
	for i, row := range rows {
		y := i + 1 // The menu belongs to the whole screen.
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

// inspectionRows keeps chat and workflow panels aligned without sharing their content or state.
func (m *model) inspectionRows(heading, hint, body string, width, bodyHeight int) []string {
	inner := max(width-2*m.promptOffset(), 1)
	rule := m.styles.dim.Render(strings.Repeat("─", inner))
	rows := []string{rule, cellLine(m.styles.gate.Render(heading), max(inner-3, 0)) + " × ", rule}
	rows = append(rows, strings.Split(cellFrame(body, inner, bodyHeight), "\n")...)
	rows = append(rows, cellLine(m.styles.dim.Render(hint), inner))
	if m.actionBarVisible() {
		rows = append(rows, rule)
	}
	if m.promptOffset() > 0 && !m.minimal {
		for i := range rows {
			l, r := "│", "│"
			switch i {
			case 0:
				l, r = "┌", "┐"
			case 2:
				l, r = "├", "┤"
			case len(rows) - 1:
				l, r = "└", "┘"
			}
			rows[i] = m.styles.dim.Render(l) + cellLine(rows[i], inner) + m.styles.dim.Render(r)
		}
	}
	if m.minimal && m.promptOffset() > 0 {
		for i := range rows {
			if i == 0 || i == 2 || (m.actionBarVisible() && i == len(rows)-1) {
				rows[i] = m.styles.dim.Render(strings.Repeat("─", width))
			} else {
				rows[i] = " " + cellLine(rows[i], inner) + " "
			}
		}
	}
	return rows
}

func (m *model) inspectorKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
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
func (m *model) selectTool(direction int) {
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
func (m *model) inspectorScrollbarMouse(msg tea.MouseMsg) bool {
	return viewportScrollbarMouse(&m.inspector.pane.Viewport, &m.inspector.pane.Follow, &m.inspector.drag, m.termW-m.promptOffset()-1, paneTop, msg)
}

func (m *model) inspectorMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	mouse := msg.Mouse()
	if m.inspectorVisible() && (m.termW < inspectorSplitWidth || mouse.X >= m.mainWidth()) {
		if mouse.Y < 1 || mouse.Y >= m.termH-m.actionBarRows() {
			return false, nil
		}
		if mouse.Button == tea.MouseLeft && isMouseClick(msg) {
			if mouse.Y == 2 && mouse.X >= m.termW-m.promptOffset()-3 {
				m.closeInspector()
				return true, nil
			}
			m.inspector.focused = true
		}
		if m.inspectorScrollbarMouse(msg) {
			return true, nil
		}
		if mouse.Button == tea.MouseWheelUp || mouse.Button == tea.MouseWheelDown {
			return true, m.inspector.pane.Update(msg)
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
func (m *model) inspectorHint() string {
	if m.inspector.selecting && m.inspector.selected >= 0 && m.inspector.selected < len(m.transcript.blocks) {
		name, _, _ := strings.Cut(m.transcript.blocks[m.inspector.selected].text, " ")
		return fmt.Sprintf("Selected %s · ↑↓ tools · Enter inspect · Esc composer", name)
	}
	return ""
}
