package agent

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFooterActionsKeepDraftAndToggleScreens(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("draft\nsecond line")
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF1})
	if m.chrome.Dialog == nil || m.chrome.Dialog.Title != "Help" {
		t.Fatal("help did not open")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF1})
	if m.chrome.Dialog != nil || m.input.Value() != "draft\nsecond line" {
		t.Fatal("help lost draft")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF2})
	if m.sessions == nil {
		t.Fatal("sessions did not open")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF2})
	if m.sessions != nil || m.input.Value() != "draft\nsecond line" {
		t.Fatal("sessions lost draft")
	}
	before := m.transcript.compact
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF3})
	if m.transcript.compact == before {
		t.Fatal("details did not toggle")
	}
}

func TestFooterActionsCannotHidePendingPrompts(t *testing.T) {
	for _, state := range []runState{stPermission, stAsk} {
		m := sizeModel(t, testModel())
		m.state = state
		before := m.transcript.compact
		for _, key := range []rune{tea.KeyF1, tea.KeyF2, tea.KeyF3, tea.KeyF4, tea.KeyF5, tea.KeyF6, tea.KeyF7} {
			m.testKey(tea.KeyPressMsg{Code: key})
		}
		if m.chrome.Dialog != nil || m.sessions != nil || m.state != state || m.transcript.compact != before {
			t.Fatal("function key hid prompt")
		}
		m.testKey(tea.KeyPressMsg{Code: tea.KeyF10})
		requests := m.TakeRequests()
		if len(requests) != 1 {
			t.Fatal("quit intent missing")
		}
		m.PrepareQuit()
		if m.quit || m.state != stStopping {
			t.Fatal("quit must first stop the active run")
		}
	}
}

func TestActionBarCellsAndMouseShareGeometry(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		for _, width := range []int{40, 80, 120} {
			m := sizeModel(t, testModel())
			m.minimal = minimal
			m.state = stDone
			m.width(tea.WindowSizeMsg{Width: width, Height: 24})
			rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
			top := len(rows) - m.actionBarRows()
			cells := m.testControls().FooterCells()
			if cells[0].Start != 0 || cells[len(cells)-1].End != m.termW {
				t.Fatal("footer edges do not align with outer frame")
			}
			for _, cell := range cells {
				bar := rows[top+cell.Row]
				if !strings.HasPrefix(string([]rune(bar)[cell.Start:]), func() string {
					if cell.End-cell.Start < 14 && strings.HasPrefix(cell.Action.Name, "Ctrl+") {
						return " ^" + strings.TrimPrefix(cell.Action.Name, "Ctrl+") + " "
					}
					return " " + strings.ToUpper(cell.Action.Name) + " "
				}()) {
					t.Fatalf("button moved at width %d: %q", width, bar)
				}
				if cell.Start > 0 && []rune(bar)[cell.Start-1] != ' ' {
					t.Fatal("button separator missing")
				}
				if cell.Action.Key == 'p' {
					m.testUpdate(tea.MouseClickMsg{X: cell.Start, Y: top + cell.Row, Button: tea.MouseLeft})
					if m.sessions == nil {
						t.Fatalf("transcript button click missed at width %d", width)
					}
				}
			}
		}
	}
}

func TestFocusAndMetricsHaveDedicatedPlaces(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.steps, m.maxSteps = 7, 25
	if strings.Contains(m.statusLine(), "step") || !strings.Contains(m.testView().Content, "STEP 7/25") {
		t.Fatal("metrics mixed with key hints")
	}
	rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
	if !strings.Contains(rows[2], "STEP 7/25") || strings.Count(strings.Join(rows, "\n"), "STEP 7/25") != 1 {
		t.Fatal("run metrics must appear once in the top status row")
	}
	if strings.Contains(ansi.Strip(m.identityLine()), "HISTORY") {
		t.Fatal("obsolete pane label remains")
	}

	m.testUpdate(tea.MouseClickMsg{X: 60, Y: 5, Button: tea.MouseLeft})
	if !m.input.Focused() {
		t.Fatal("click did not return focus to input")
	}
}

func TestHeaderPrioritizesRunMetricsOnNarrowTerminals(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	m.steps, m.maxSteps = 2, 25
	m.tokens, m.limit = 4100, 16400
	m.elapsed = 32 * time.Second
	m.toolsUsed = 1
	m.modelName = "Qwen3.5-9B-Q4_K_M"
	for _, width := range []int{80, 120} {
		m.width(tea.WindowSizeMsg{Width: width, Height: 30})
		line := ansi.Strip(m.identityLine())
		for _, fact := range []string{"STEP 2/25", "4.1k/16.4k", "00:32"} {
			if !strings.Contains(line, fact) {
				t.Fatalf("header at width %d lost %q: %q", width, fact, line)
			}
		}
		if strings.Contains(line, "context ") {
			t.Fatal("context limit duplicated beside run metrics")
		}
	}
}

func TestHelpKeepsFooterOnLastRowAndHidesComposer(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("first\nsecond\nthird")
	m.fitInput()
	m.fitBottom()
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF1})
	rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
	if !strings.Contains(rows[len(rows)-1], "^Q Quit") || strings.Contains(ansi.Strip(m.testView().Content), "first") || !strings.Contains(m.testView().Content, "Draft preserved") {
		t.Fatal("help moved footer or exposed active composer")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF1})
	if m.input.Value() != "first\nsecond\nthird" || m.input.Height() != 3 || !m.input.Focused() {
		t.Fatal("help did not restore composer")
	}
}

func TestFooterModeAndNewPreserveDraft(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("unsent task")
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF5})
	if !m.plan || m.input.Value() != "unsent task" {
		t.Fatal("mode change lost draft")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF5})
	if m.plan {
		t.Fatal("mode did not toggle back")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyF4})
	if m.input.Value() != "unsent task" {
		t.Fatal("new on unsaved session lost draft")
	}
}

func TestToolbarContextAndQuitProtection(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.transcript.Follow = true
	actions := m.footerActions()
	if actions[0].Label != "Help" || actions[1].Label != "Search" || actions[2].Label != "New" || actions[3].Label != "Send" || actions[4].Label != "Newline" {
		t.Fatal("composer actions")
	}
	m.input.SetValue("draft")
	m.testKey(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if !strings.Contains(m.input.Value(), "\n") {
		t.Fatal("toolbar newline shortcut")
	}
	m.openSessions()
	if m.footerActions()[3].Label != "Rename" {
		t.Fatal("navigator actions")
	}
	m.closeSessions()
	m.state = stRunning
	if m.footerActions()[0].Label != "Stop" {
		t.Fatal("run actions")
	}
	m.testKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	ready := m.PrepareQuit()
	_ = ready
	if m.quit || m.state != stStopping {
		t.Fatal("active run quit without stopping")
	}
	m.testKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	ready = m.PrepareQuit()
	_ = ready
	if m.quit {
		t.Fatal("quit before stop completed")
	}
	m.state = stDone
	m.testKey(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	ready = m.PrepareQuit()
	if len(m.TakeRequests()) != 3 {
		t.Fatal("missing quit intents")
	}
	if !ready {
		t.Fatal("idle quit")
	}
}

func TestSearchToolbarShortcutClosesPopup(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	key := tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	m.testKey(key)
	if m.sessions == nil {
		t.Fatal("search shortcut did not open")
	}
	m.testKey(key)
	if m.sessions != nil {
		t.Fatal("search shortcut did not close")
	}
}

func TestDetailsToolbarRemainsVisibleAndClickable(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.width(tea.WindowSizeMsg{Width: 160, Height: 30})
	for _, compact := range []bool{true, false} {
		m.transcript.compact = compact
		label := "Thinking"
		found := false
		for _, cell := range m.testControls().FooterCells() {
			if cell.Action.Key != 'g' {
				continue
			}
			found = true
			if cell.Action.Label != label || !cell.Action.Enabled {
				t.Fatal("incorrect details action")
			}
			m.testUpdate(tea.MouseClickMsg{X: cell.Start, Y: m.termH - m.actionBarRows() + cell.Row, Button: tea.MouseLeft})
			if m.transcript.compact == compact {
				t.Fatal("details click did not toggle")
			}
		}
		if !found {
			t.Fatal("details action missing")
		}
	}
}

func TestSlashQuitWaitsForRunCompletion(t *testing.T) {
	for _, command := range []string{"/quit", "/q"} {
		t.Run(command, func(t *testing.T) {
			m := sizeModel(t, testModel())
			m.state = stRunning
			cancelled := false
			m.cancel = func() { cancelled = true }
			m.command(command)
			m.PrepareQuit()
			if !cancelled || m.quit || m.state != stStopping {
				t.Fatal("quit did not wait for cancellation")
			}
			m.command(command)
			m.PrepareQuit()
			if m.quit {
				t.Fatal("quit while worker still running")
			}
			m.testUpdate(doneMsg{})
			m.command(command)
			if !m.PrepareQuit() || len(m.TakeRequests()) != 3 {
				t.Fatal("completed worker prevented quit")
			}
		})
	}
}
