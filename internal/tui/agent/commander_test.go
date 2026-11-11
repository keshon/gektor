package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/screen"
)

func TestCommanderFrameAtSupportedSizes(t *testing.T) {
	for _, width := range []int{40, 80, 120, 160} {
		for _, state := range []runState{stDone, stRunning, stPermission, stAsk} {
			m := sizeModel(t, testModel())
			m.state = state
			m.width(tea.WindowSizeMsg{Width: width, Height: 30})
			rows := strings.Split(m.testView().Content, "\n")
			if len(rows) != 30 || m.footerRows() != 2 || len(strings.Split(m.testControls().MenuLine(), "\n")) != 1 {
				t.Fatalf("%d state %d: bad row budget", width, state)
			}
			for _, row := range rows {
				if lipgloss.Width(row) != width {
					t.Fatalf("%d state %d: row width %d", width, state, lipgloss.Width(row))
				}
			}
			if strings.Contains(ansi.Strip(rows[2]), "SESSIONS") {
				t.Fatal("removed session pane still visible")
			}
			want := 21
			if state == stRunning {
				want++
			}
			if m.transcript.Viewport.Height() != want {
				t.Fatalf("lost transcript rows: %d", m.transcript.Viewport.Height())
			}
		}
	}
}

func TestCommanderMenuClickPreservesDraftAndGate(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.input.SetValue("unfinished draft")
	cell := m.testControls().MenuCells()[0]
	m.testUpdate(tea.MouseClickMsg{X: cell.Start, Y: 0, Button: tea.MouseLeft})
	if m.chrome.Header.Menu != "session" || !m.chrome.Header.Global || m.input.Value() != "unfinished draft" {
		t.Fatal("menu failed to open or lost draft")
	}
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.testKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.state = stPermission
	m.testUpdate(tea.MouseClickMsg{X: cell.Start, Y: 0, Button: tea.MouseLeft})
	if m.chrome.Header.Focused || m.state != stPermission {
		t.Fatal("menu hid approval")
	}
}

// Optional export uses the real frame renderer with fixture content, no model.
func TestCommanderPreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview export opt-in")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	preview := settingsModel(t)
	preview.width(tea.WindowSizeMsg{Width: 120, Height: 34})
	preview.testControls().OpenForm("Model", "Choose the thinking model.", "Apply", []form.Field{{Key: "kind", Label: "Protocol", Value: "llama", Multiline: false}, {Key: "endpoint", Label: "Backend URL", Value: "http://127.0.0.1:8080", Multiline: false}, {Key: "model", Label: "Model identifier", Value: "qwen3-1.7b", Multiline: false}}, func(map[string]string) error { return nil })
	if err := os.WriteFile(filepath.Join(dir, "model-form.txt"), []byte(ansi.Strip(preview.testView().Content)), 0600); err != nil {
		t.Fatal(err)
	}
	preview.testControls().Close()
	preview.live = "<think>Consider the question carefully.</think>Hello **User**. How are you today?"
	preview.refreshContent()
	if err := os.WriteFile(filepath.Join(dir, "streaming.txt"), []byte(ansi.Strip(preview.testView().Content)), 0600); err != nil {
		t.Fatal(err)
	}

	for _, size := range []struct {
		name string
		w, h int
	}{{"wide", 120, 30}, {"narrow", 80, 24}} {
		m := settingsModel(t)
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
		if err := os.WriteFile(filepath.Join(dir, size.name+".txt"), []byte(ansi.Strip(m.testView().Content)), 0600); err != nil {
			t.Fatal(err)
		}
		if size.name == "wide" {
			m.chrome.Header = screen.MenuState{Focused: true, Global: true, Index: 4, Menu: "session"}
			if err := os.WriteFile(filepath.Join(dir, "settings.txt"), []byte(ansi.Strip(m.testView().Content)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCommanderOutlinesCloseOnBothSides(t *testing.T) {
	m := sizeModel(t, testModel())
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})
	rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
	if !strings.HasPrefix(rows[1], "┌") || !strings.HasSuffix(rows[1], "┐") || strings.Contains(rows[1], "┬") {
		t.Fatal("top outline is open")
	}
	for i, row := range rows[2:28] {
		if i == 1 {
			if !strings.HasPrefix(row, "├") || !strings.HasSuffix(row, "┤") || strings.Contains(row, "┼") {
				t.Fatal("header rule disconnected")
			}
			continue
		}
		if (!strings.HasPrefix(row, "│") || !strings.HasSuffix(row, "│")) &&
			(!strings.HasPrefix(row, "├") || !strings.HasSuffix(row, "┤")) {
			t.Fatal("side outline is open")
		}
	}
	if !strings.HasPrefix(rows[28], "└") || !strings.HasSuffix(rows[28], "┘") || strings.Contains(rows[28], "┴") {
		t.Fatal("bottom outline is open")
	}
}
