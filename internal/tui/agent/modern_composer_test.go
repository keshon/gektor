package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/pkg/tui/components/popup"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestModernQueueRowTracksComposerPadding(t *testing.T) {
	defer screen.SetModern(false)
	for _, width := range []int{40, 80, 120} {
		m := queuedModel(t)
		m.testUpdate(tea.WindowSizeMsg{Width: width, Height: 30})
		m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
		before := m.sess.PendingInputs()
		rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
		queueY := -1
		for y, row := range rows {
			if strings.Contains(row, "Queued 3") {
				queueY = y
				break
			}
		}
		if queueY < 0 || strings.Contains(rows[queueY], "Latest:") {
			t.Fatal("queue summary is absent or uses the old layout", width)
		}
		caret := m.testView().Cursor
		if caret == nil || caret.X < m.promptOffset() || caret.Y <= queueY {
			t.Fatal("queue and input caret disagree", width, caret, queueY)
		}
		x := m.promptOffset() + 12
		m.testUpdate(tea.MouseClickMsg{X: x, Y: queueY, Button: tea.MouseLeft})
		if !testTableOpen(m) || len(m.sess.PendingInputs()) != len(before) || m.input.Value() != "unfinished draft" || len(m.draftImages) != 1 {
			t.Fatal("queue row missed its target or changed the draft", width)
		}
	}
}

func TestModernInspectorOmitsHints(t *testing.T) {
	defer screen.SetModern(false)
	m := inspectorModel(t, 120)
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	hints := []popup.Shortcut{{Key: "Esc", Action: "Close"}, {Key: "Tab", Action: "Parent"}}
	if m.inspectionHints(60, hints...) != "" {
		t.Fatal("Modern inspector still has hints")
	}
	m.applyAppearance(config.Appearance{Style: "commander", Frame: "minimal"})
	if !strings.Contains(ansi.Strip(m.inspectionHints(60, hints...)), "ESC") {
		t.Fatal("Commander lost its hints")
	}
}

func TestModernQueuePreview(t *testing.T) {
	dir := os.Getenv("GEKTOR_TUI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("preview export opt-in")
	}
	defer screen.SetModern(false)
	m := queuedModel(t)
	m.testUpdate(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	m.modelName = "Ornith-1.5-35B-A3B"
	m.appendBlock(userBlock("Check the queue, message rail and input spacing."))
	err := os.WriteFile(filepath.Join(dir, "modern-queue.ansi"), []byte(m.testView().Content), 0600)
	if err != nil {
		t.Fatal(err)
	}
}

func TestModernMessageHeadingUsesSquareSeparator(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModelTo(t, testModel(), 80, 30)
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	m.appendBlock(userBlock("Check the heading"))
	view := ansi.Strip(m.testView().Content)
	if !strings.Contains(view, " ▪ Request") || strings.Contains(view, " · Request") {
		t.Fatal("Modern heading does not use the square separator")
	}
}

func TestModernBordersReplaceOuterComposerPadding(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModelTo(t, testModel(), 80, 30)
	m.state = stDone
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	m.input.SetValue("Keep this draft")
	m.fitBottom()
	borderless := len(m.composerRows())
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "outlined"})
	rows := m.composerRows()
	if len(rows) != borderless-2 || !strings.Contains(ansi.Strip(rows[0]), "Keep this draft") || !strings.Contains(ansi.Strip(rows[len(rows)-1]), "Act") {
		t.Fatal("visible borders did not replace the outer padding")
	}
	view := m.testView()
	screenRows := strings.Split(ansi.Strip(view.Content), "\n")
	if view.Cursor == nil || !strings.Contains(screenRows[view.Cursor.Y], "Keep this draft") || !strings.Contains(screenRows[m.Presentation().BadgeRow], "Act") {
		t.Fatal("compact composer cursor or status menu anchor drifted")
	}
}

func TestModernHistoryTextSharesLeftAlignment(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModelTo(t, testModel(), 80, 30)
	m.state = stDone
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	m.appendBlock(userBlock("User alignment"))
	m.appendBlock(markerBlock("System alignment"))
	m.appendBlock(thinkBlock("Thinking alignment"))
	m.appendBlock(answerBlock("Reply alignment"))
	rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
	column := -1
	for _, text := range []string{"User alignment", "System alignment", "Thinking alignment", "Reply alignment"} {
		found := false
		for _, row := range rows {
			x := strings.Index(row, text)
			if x < 0 {
				continue
			}
			x = ansi.StringWidth(row[:x])
			if column < 0 {
				column = x
			}
			if x != column {
				t.Fatal("history text alignment differs", text, x, column)
			}
			found = true
			break
		}
		if !found {
			t.Fatal("history text missing", text)
		}
	}
}
