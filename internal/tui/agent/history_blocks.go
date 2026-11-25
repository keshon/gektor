package agent

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/pkg/tui/components/cell"
	"github.com/keshon/gektor/pkg/tui/components/table"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func (m *Model) blockExpanded(index int) bool {
	if value, ok := m.transcript.expanded[index]; ok {
		return value
	}
	if m.transcript.blocks[index].role == roleThink {
		return !m.transcript.compact
	}
	return !m.hideTools && !m.toolNamesOnly
}

func (m *Model) toggleHistoryBlock(index int) {
	if !currentPalette().Modern || index < 0 || index >= len(m.transcript.blocks) {
		return
	}
	role := m.transcript.blocks[index].role
	if role != roleThink && role != roleTool {
		return
	}
	expanded := m.blockExpanded(index)
	if m.transcript.expanded == nil {
		m.transcript.expanded = make(map[int]bool)
	}
	m.transcript.expanded[index] = !expanded
	m.inspector.focused = false
	m.transcript.Follow = false
	m.refreshContent()
}

func (m *Model) expansionMouse(msg tea.MouseMsg) bool {
	mouse := msg.Mouse()
	if !currentPalette().Modern || !isMouseClick(msg) || mouse.Button != tea.MouseLeft || mouse.X == m.scrollbarX() || mouse.X < m.promptOffset() || mouse.X >= m.mainWidth() || mouse.Y < m.transcriptTop() || mouse.Y >= m.transcriptTop()+m.transcript.Viewport.Height() {
		return false
	}
	m.inspector.selecting = false
	row := mouse.Y - m.transcriptTop() + m.transcript.Viewport.YOffset()
	x := mouse.X - m.promptOffset()
	for _, hit := range m.transcript.render.inlineTools {
		if row == m.transcript.PartOffset(hit.part)+hit.row && x == hit.start {
			m.toggleHistoryBlock(hit.block)
			return true
		}
	}
	for part, index := range m.transcript.render.partBlocks {
		b := m.transcript.blocks[index]
		top := m.transcript.PartOffset(part)
		if b.role == roleThink && row == top && x >= 2 {
			m.toggleHistoryBlock(index)
			return true
		}
		if hit, ok := m.transcript.render.toolHits[part]; ok && row == top+hit.row && x == hit.start-2 {
			m.toggleHistoryBlock(index)
			return true
		}
	}
	return false
}

func (m *Model) openHistoryBlocks() {
	rows := []table.Row{}
	for i, b := range m.transcript.blocks {
		if b.role != roleThink && b.role != roleTool {
			continue
		}
		if b.role == roleThink && m.hideThinking {
			continue
		}
		label := "Thinking"
		if b.role == roleTool {
			name, action, target := toolSummary(b.text)
			label = action + " " + target
			if action == "" {
				label = name
			}
		}
		detail := "Collapsed"
		if m.blockExpanded(i) {
			detail = "Expanded"
		}
		if b.role == roleThink {
			preview := cell.Truncate(strings.Join(strings.Fields(b.text), " "), 90)
			detail += " ▪ " + preview
		}
		rows = append(rows, table.Row{ID: strconv.Itoa(i), Cells: []string{label}, Detail: detail})
	}
	m.chrome.PresentDialog(screen.Dialog{Title: "History blocks", Table: &screen.Table{State: m.chrome.NewTable(), List: true,
		Description: "Choose a block to expand or collapse. Saved history stays unchanged.", Columns: []table.Column{{Label: "Block"}}, Rows: rows,
		Actions: []table.Action{{ID: "details", Label: "Toggle", Key: "Enter", Enabled: true}}, EmptyMessage: "No thinking or tool blocks"}})
	m.blockDialog = m.chrome.DialogID()
}

func (m *Model) activateHistoryBlock(id string) {
	index, err := strconv.Atoi(id)
	if err != nil {
		return
	}
	m.chrome.Close()
	m.inspector.selecting = false
	m.toggleHistoryBlock(index)
	for part, block := range m.transcript.render.partBlocks {
		if block == index {
			m.transcript.Viewport.SetYOffset(m.transcript.PartOffset(part))
			break
		}
	}
}
