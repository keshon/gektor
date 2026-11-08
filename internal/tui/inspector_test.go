package tui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/run"
	"github.com/keshon/gektor/internal/session"
)

func inspectorModel(t *testing.T, width int) *model {
	m := sizeModelTo(t, testModel(), width, 30)
	m.state = stDone
	m.input.SetValue("parent draft")
	m.appendBlock(toolCardBlock("read-1", `read_file {"path":"main.go"}`))
	m.attachResult("read-1", strings.Repeat("line of output\n", 70))
	return m
}

func TestInspectorRemainsVisibleBehindMainMenu(t *testing.T) {
	for _, width := range []int{70, 120} {
		for _, minimal := range []bool{false, true} {
			m := inspectorModel(t, width)
			m.minimal = minimal
			m.openInspector(len(m.transcript.blocks) - 1)
			m.inspector.pane.Viewport.SetYOffset(10)
			beforeWidth := m.mainWidth()
			beforeOffset := m.inspector.pane.Viewport.YOffset()
			for _, menu := range []string{"session", "execution", "view", "settings", "help"} {
				m.header = headerState{focused: true, global: true, menu: menu}
				m.fitBottom()
				view := ansi.Strip(m.View().Content)
				if !m.inspectorVisible() || m.mainWidth() != beforeWidth || !strings.Contains(view, "line of output") {
					t.Fatalf("menu %s hid inspector or changed layout: width=%d minimal=%v", menu, width, minimal)
				}
				if menu == "settings" && !strings.Contains(view, "Appearance") {
					t.Fatal("inspector painted over menu")
				}
				m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
				if m.inspector.pane.Viewport.YOffset() != beforeOffset {
					t.Fatal("menu navigation scrolled inspector")
				}
				m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
				if !m.inspectorVisible() || m.inspector.callID != "read-1" {
					t.Fatal("closing menu closed inspector")
				}
			}
		}
	}
}

func TestInspectorScrollbarMouseMatchesBothLayouts(t *testing.T) {
	for _, width := range []int{70, 120} {
		for _, minimal := range []bool{false, true} {
			m := inspectorModel(t, width)
			m.minimal = minimal
			m.openInspector(len(m.transcript.blocks) - 1)
			v := &m.inspector.pane.Viewport
			v.GotoTop()
			m.inspector.pane.Follow = false
			x := m.termW - m.promptOffset() - 1
			rows := strings.Split(ansi.Strip(m.View().Content), "\n")
			if !strings.HasSuffix(ansi.Truncate(rows[paneTop], x+1, ""), "█") {
				t.Fatal("hit target differs from thumb", width, minimal, rows[paneTop])
			}
			parent := m.transcript.Viewport.YOffset()
			_, size := v.ThumbRange(v.Height())
			m.handleMouse(tea.MouseClickMsg{X: x, Y: paneTop + size, Button: tea.MouseLeft})
			m.handleMouse(tea.MouseReleaseMsg{X: x, Y: paneTop + size, Button: tea.MouseLeft})
			if v.YOffset() == 0 {
				t.Fatal("track click did not page inspector", width, minimal)
			}
			v.GotoTop()
			m.handleMouse(tea.MouseClickMsg{X: x, Y: paneTop, Button: tea.MouseLeft})
			m.handleMouse(tea.MouseMotionMsg{X: 0, Y: m.termH + 4, Button: tea.MouseLeft})
			if !v.AtBottom() {
				t.Fatal("drag outside pane did not reach bottom")
			}
			m.handleMouse(tea.MouseReleaseMsg{X: 0, Y: m.termH + 4, Button: tea.MouseLeft})
			if m.inspector.drag.active || m.transcript.Viewport.YOffset() != parent {
				t.Fatal("drag leaked into parent")
			}
		}
	}
}

func TestCollapsedFailureAndBackgroundRemainInspectable(t *testing.T) {
	m := inspectorModel(t, 120)
	m.hideTools = true
	m.hideNames = true
	m.hideLabels = true
	text := harnessPrefix + "Background process bg1 completed successfully. Historical command output:\nPRIVATE_BACKGROUND"
	m.appendBlock(contextBlock(llm.Message{Role: llm.RoleUser, Content: text}))
	index := len(m.transcript.blocks) - 1
	if m.transcript.blocks[index].role != roleTool {
		t.Fatal("background is not inspectable")
	}
	if strings.Contains(ansi.Strip(m.transcript.View()), "PRIVATE_BACKGROUND") {
		t.Fatal("collapsed background leaked")
	}
	m.openInspector(index)
	if !strings.Contains(ansi.Strip(m.inspector.pane.View()), "PRIVATE_BACKGROUND") {
		t.Fatal("inspector lost background output")
	}
	m.closeInspector()
	blocks, _ := renderHistoryAt([]llm.Message{{Role: llm.RoleUser, Content: text}})
	if len(blocks) != 1 || blocks[0].callID != m.transcript.blocks[index].callID {
		t.Fatal("reload lost background card")
	}
	m.appendBlock(toolCardBlock("failed", `run_program {"program":"go","args":["test"]}`))
	m.attachResult("failed", "PRIVATE_DOWNLOAD_LOG\nerror: process timed out", true)
	m.transcript.Viewport.GotoBottom()
	view := ansi.Strip(m.transcript.View())
	if strings.Contains(view, "PRIVATE_DOWNLOAD_LOG") || !strings.Contains(view, "process timed out") {
		t.Fatal(view)
	}
	for part, blockIndex := range m.transcript.render.partBlocks {
		if blockIndex == len(m.transcript.blocks)-1 {
			hit, ok := m.transcript.render.toolHits[part]
			if !ok {
				t.Fatal("failed summary is not clickable")
			}
			y := paneTop + m.transcript.PartOffset(part) + hit.row - m.transcript.Viewport.YOffset()
			m.handleMouse(tea.MouseClickMsg{X: m.promptOffset() + hit.start, Y: y, Button: tea.MouseLeft})
		}
	}
	if !m.inspectorVisible() || !strings.Contains(ansi.Strip(m.inspector.pane.View()), "PRIVATE_DOWNLOAD_LOG") {
		t.Fatal("error inspection lost output")
	}
}
func TestInspectorSplitKeepsComposerAndScrollIndependent(t *testing.T) {
	m := inspectorModel(t, 120)
	m.openInspector(len(m.transcript.blocks) - 1)
	m.syncFocus()
	if m.mainWidth() != 60 || m.activeInputOwner() != focusInspector {
		t.Fatal("split/focus", m.mainWidth(), m.activeInputOwner())
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Inspect") || !strings.Contains(view, "parent draft") {
		t.Fatal(view)
	}
	parentOffset := m.transcript.Viewport.YOffset()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyHome})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.inspector.pane.Viewport.YOffset() != 1 || m.transcript.Viewport.YOffset() != parentOffset {
		t.Fatal("scroll leaked")
	}
	m.Update(tea.PasteMsg{Content: "must not edit"})
	if m.input.Value() != "parent draft" {
		t.Fatal("inspector accepted input")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.activeInputOwner() != focusInput {
		t.Fatal("cannot return to parent")
	}
	m.Update(tea.PasteMsg{Content: " more"})
	if !strings.Contains(m.input.Value(), "more") {
		t.Fatal("composer stopped accepting input")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.inspectorVisible() || m.mainWidth() != 120 {
		t.Fatal("inspector did not close")
	}
}
func TestInspectorNarrowAndGateOwnership(t *testing.T) {
	m := inspectorModel(t, 70)
	m.openInspector(len(m.transcript.blocks) - 1)
	m.syncFocus()
	view := ansi.Strip(m.View().Content)
	if strings.Contains(view, "parent draft") || !strings.Contains(view, "Inspect") {
		t.Fatal(view)
	}
	if m.View().Cursor != nil {
		t.Fatal("composer cursor leaked into inspector")
	}
	m.state = stPermission
	m.fitBottom()
	if m.inspectorVisible() || m.activeInputOwner() == focusInspector {
		t.Fatal("inspector obscures gate")
	}
	m.state = stDone
	m.fitBottom()
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.input.Value() != "parent draft" {
		t.Fatal("draft lost")
	}
}
func TestInspectorKeyboardSelectsToolAndDoesNotSubmit(t *testing.T) {
	m := inspectorModel(t, 120)
	m.handleKey(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	m.syncFocus()
	if !m.inspector.selecting || m.activeInputOwner() != focusViewport {
		t.Fatal("selection not focused")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.inspectorVisible() || m.input.Value() != "parent draft" {
		t.Fatal("enter submitted instead of inspecting")
	}
}
func TestInspectorChildEventsStayOutOfParentAndReload(t *testing.T) {
	m := inspectorModel(t, 120)
	m.stateFile = filepath.Join(t.TempDir(), "state.json")
	m.appendBlock(toolCardBlock("child-1", `delegate_task {"task":"check code"}`))
	parentCount := len(m.transcript.blocks)
	m.tokens = 123
	msg := llm.Message{Role: llm.RoleAssistant, Content: "child answer", Reasoning: "child summary"}
	raw, _ := json.Marshal(msg)
	var decoded any
	_ = json.Unmarshal(raw, &decoded)
	m.handleEvent(run.Event{Name: "child", Fields: map[string]any{"call_id": "child-1", "kind": "message", "fields": map[string]any{"message": decoded}}})
	if len(m.transcript.blocks) != parentCount || m.tokens != 123 {
		t.Fatal("child corrupted parent")
	}
	m.openInspector(parentCount - 1)
	if !strings.Contains(ansi.Strip(m.View().Content), "child answer") {
		t.Fatal("child missing from inspector")
	}
	saved := session.New([]llm.Message{{Role: llm.RoleSystem, Content: "system"}, {Role: llm.RoleUser, Content: "check code"}, msg})
	if err := saved.Save(session.ChildStatePath(m.stateFile, "child-1")); err != nil {
		t.Fatal(err)
	}
	m.inspector = toolInspector{}
	cmd := m.openInspector(parentCount - 1)
	if cmd == nil {
		t.Fatal("no history load")
	}
	m.Update(cmd())
	if !strings.Contains(ansi.Strip(m.View().Content), "child answer") {
		t.Fatal("saved child missing")
	}
	m.acceptChildHistory(childLoadedMsg{parent: "other session", callID: "child-1", snapshot: session.New(nil)})
	if len(m.child("child-1").history) == 0 {
		t.Fatal("stale load overwrote child")
	}
}
func TestInspectorRenderFitsAllThemesAndWidths(t *testing.T) {
	defer setTheme("dark")
	for _, theme := range config.ThemeNames() {
		for _, width := range []int{60, 100, 160} {
			setTheme(theme)
			m := inspectorModel(t, width)
			m.openInspector(len(m.transcript.blocks) - 1)
			view := m.View().Content
			if lipgloss.Height(view) != 30 {
				t.Fatalf("height %d", lipgloss.Height(view))
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) != width {
					t.Fatalf("%s width %d got %d", theme, width, lipgloss.Width(line))
				}
			}
		}
	}
}

func TestInspectorMouseUsesRenderedToolRowsAndCloseButton(t *testing.T) {
	m := inspectorModel(t, 120)
	m.hideSYS = true
	m.refreshContent()
	m.transcript.Viewport.GotoTop()
	var y int
	for part, index := range m.transcript.render.partBlocks {
		if m.transcript.blocks[index].callID == "read-1" {
			y = paneTop + m.transcript.PartOffset(part) + m.transcript.render.toolHits[part].row
			break
		}
	}
	m.Update(tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})
	if !m.inspectorVisible() {
		t.Fatal("tool click did not open inspector")
	}
	m.Update(tea.MouseClickMsg{X: 118, Y: 2, Button: tea.MouseLeft})
	if m.inspectorVisible() || m.header.menu != "" {
		t.Fatal("close click reached header")
	}
}

func TestInspectorArgumentsPreserveNumbers(t *testing.T) {
	got := prettyToolArguments(`tool {"id":9007199254740993}`)
	if !strings.Contains(got, "9007199254740993") {
		t.Fatal(got)
	}
}

func TestInspectorSharesMenuAndFrame(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		m := inspectorModel(t, 120)
		m.minimal = minimal
		m.fitBottom()
		menu := ansi.Strip(m.menuLine())
		m.openInspector(len(m.transcript.blocks) - 1)
		rows := strings.Split(ansi.Strip(m.View().Content), "\n")
		if strings.TrimSpace(rows[0]) != strings.TrimSpace(menu) {
			t.Fatal("menu changed", rows[0])
		}
		if strings.Contains(rows[0], "Inspect") || !strings.Contains(rows[2], "Inspect") {
			t.Fatal("inspector header misplaced")
		}
		if !minimal {
			for _, y := range []int{1, m.termH - m.footerRows()} {
				cells := []rune(rows[y])
				if cells[0] != cells[60] || cells[59] != cells[119] {
					t.Fatalf("unequal frames at %d: %s", y, rows[y])
				}
			}
		} else {
			if strings.Contains(rows[1], "┌") {
				t.Fatal("outline ignored")
			}
			for y := 1; y < m.termH-m.actionBarRows(); y++ {
				cells := []rune(rows[y])
				if y == 1 || y == 3 || y == m.termH-m.footerRows() {
					if cells[0] != '─' || cells[60] != '─' || cells[119] != '─' {
						t.Fatalf("rule does not span margins: %q", rows[y])
					}
					continue
				}
				if y == paneTop+m.transcript.Viewport.Height() {
					continue
				}
				if y == m.termH-m.footerRows()-2 {
					if cells[60] != '─' || cells[119] != '─' {
						t.Fatal("inspector footer divider missing", rows[y])
					}
					continue
				}
				if cells[0] != ' ' || cells[60] != ' ' || cells[119] != ' ' {
					t.Fatalf("missing gutter at row %d: %q", y, rows[y])
				}
			}
		}
	}
}
func TestInspectorOnlyToolNameIsClickable(t *testing.T) {
	for _, compact := range []bool{false, true} {
		m := inspectorModel(t, 120)
		m.hideTools = compact
		m.refreshContent()
		m.transcript.Viewport.GotoTop()
		for part, hit := range m.transcript.render.toolHits {
			y := paneTop + m.transcript.PartOffset(part) + hit.row
			for _, pos := range [][2]int{{hit.end + m.promptOffset() + 2, y}, {119, y}, {5, y + 1}} {
				handled, _ := m.inspectorMouse(tea.MouseClickMsg{X: pos[0], Y: pos[1], Button: tea.MouseLeft})
				if handled || m.inspectorVisible() {
					t.Fatal("non-name opened inspector", pos)
				}
			}
			handled, _ := m.inspectorMouse(tea.MouseClickMsg{X: hit.start + m.promptOffset(), Y: y, Button: tea.MouseLeft})
			if !handled || !m.inspectorVisible() {
				t.Fatal("name did not open")
			}
			break
		}
	}
}
