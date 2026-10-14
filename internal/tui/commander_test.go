package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommanderFrameAtSupportedSizes(t *testing.T) {
	for _, width := range []int{40, 80, 120, 160} {
		for _, state := range []runState{stDone, stRunning, stPermission, stAsk} {
			m := sizeModel(t, testModel())
			m.state = state
			m.width(tea.WindowSizeMsg{Width: width, Height: 30})
			rows := strings.Split(m.View().Content, "\n")
			if len(rows) != 30 || m.footerRows() != 2 || len(strings.Split(m.menuLine(), "\n")) != 1 {
				t.Fatalf("%d state %d: bad row budget", width, state)
			}
			for _, row := range rows {
				if lipgloss.Width(row) != width {
					t.Fatalf("%d state %d: row width %d", width, state, lipgloss.Width(row))
				}
			}
			if width >= navigatorMinWidth && !strings.Contains(ansi.Strip(rows[2]), "SESSIONS") {
				t.Fatal("missing session pane header")
			}
			if m.vp.Height() != 20 {
				t.Fatalf("lost transcript rows: %d", m.vp.Height())
			}
		}
	}
}

func TestCommanderMenuClickPreservesDraftAndGate(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("unfinished draft")
	cell := m.menuCells()[0]
	m.Update(tea.MouseClickMsg{X: cell.start, Y: 0, Button: tea.MouseLeft})
	if m.header.menu != "session" || !m.header.global || m.input.Value() != "unfinished draft" {
		t.Fatal("menu failed to open or lost draft")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.state = stPermission
	m.Update(tea.MouseClickMsg{X: cell.start, Y: 0, Button: tea.MouseLeft})
	if m.header.focused || m.state != stPermission {
		t.Fatal("menu hid approval")
	}
}

// Optional export uses the real frame renderer with fixture content, no model.
func TestCommanderPreview(t *testing.T) {
	dir := os.Getenv("TARS_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview export opt-in")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	preview := settingsModel(t)
	preview.width(tea.WindowSizeMsg{Width: 120, Height: 34})
	preview.openForm("Speech services", "Speech settings apply to the next voice service.", "Apply", []formField{{"microphone", "Microphone", "Microphone (Storm Pro)", false}, {"variant", "Chatterbox variant", "nano", false}, {"model", "Speech model directory", "assets/models/speaking/chatterbox/nano", false}, {"max_seconds", "Maximum recording seconds", "120", false}}, func(map[string]string) error { return nil })
	if err := os.WriteFile(filepath.Join(dir, "speech-form.txt"), []byte(ansi.Strip(preview.View().Content)), 0600); err != nil {
		t.Fatal(err)
	}
	preview.closeDialog()
	preview.live = "<think>Consider the question carefully.</think>Hello **User**. How are you today?"
	preview.refreshContent()
	if err := os.WriteFile(filepath.Join(dir, "streaming.txt"), []byte(ansi.Strip(preview.View().Content)), 0600); err != nil {
		t.Fatal(err)
	}

	for _, size := range []struct {
		name string
		w, h int
	}{{"wide", 120, 30}, {"narrow", 80, 24}} {
		m := sizeModel(t, testModel())
		m.state = stDone
		m.modelName = "Qwen3.5-9B"
		m.tokens = 4100
		m.limit = 16384
		m.width(tea.WindowSizeMsg{Width: size.w, Height: size.h})
		m.appendBlock(userBlock("Inspect the startup sequence and explain the process ownership."))
		m.appendBlock(answerBlock("The application owns the processes it starts. External backends remain independently managed. I can inspect the launcher next."))
		m.input.SetValue("Show me the launcher")
		m.fitInput()
		m.fitBottom()
		if err := os.WriteFile(filepath.Join(dir, size.name+".txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
			t.Fatal(err)
		}
		if size.name == "wide" {
			m.header = headerState{focused: true, global: true, index: 3, menu: "settings"}
			if err := os.WriteFile(filepath.Join(dir, "settings.txt"), []byte(ansi.Strip(m.View().Content)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestGlobalDropdownNavigationAndSettings(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	line := ansi.Strip(m.menuLine())
	if strings.Contains(line, "TARS") || strings.Contains(line, "F1") || !strings.Contains(line, "Settings") {
		t.Fatal(line)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyF9})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.header.menu != "session" {
		t.Fatal(m.header)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.header.menu != "conversation-mode" {
		t.Fatal("left/right failed to switch dropdown")
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyRight})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.plan || m.header.focused {
		t.Fatal("Plan selection failed")
	}
	settings := m.menuCells()[4]
	m.Update(tea.MouseClickMsg{X: settings.start, Y: 0, Button: tea.MouseLeft})
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.header.menu != "settings" || m.header.row != 0 {
		t.Fatal("settings page failed")
	}
}

func TestCommanderOutlinesCloseOnBothSides(t *testing.T) {
	m := sizeModel(t, testModel())
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	if !strings.HasPrefix(rows[1], "┌") || !strings.HasSuffix(rows[1], "┐") || !strings.Contains(rows[1], "┬") {
		t.Fatal("top outline is open")
	}
	for i, row := range rows[2:28] {
		if i == 1 {
			if !strings.HasPrefix(row, "├") || !strings.HasSuffix(row, "┤") || !strings.Contains(row, "┼") {
				t.Fatal("header rule disconnected")
			}
			continue
		}
		if !strings.HasPrefix(row, "│") || !strings.HasSuffix(row, "│") {
			t.Fatal("side outline is open")
		}
	}
	if !strings.HasPrefix(rows[28], "└") || !strings.HasSuffix(rows[28], "┘") || !strings.Contains(rows[28], "┴") {
		t.Fatal("bottom outline is open")
	}
}
