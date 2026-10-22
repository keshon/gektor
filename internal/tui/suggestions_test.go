package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/workspace"
)

func completionModel(t *testing.T) *model {
	t.Helper()
	m := sizeModel(t, testModel())
	m.state = stDone
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.ws = ws
	return m
}

func writeReference(t *testing.T, m *model, name string, data []byte) {
	t.Helper()
	path := filepath.Join(m.ws.Root(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommandSuggestionsInsertWithoutSubmitting(t *testing.T) {
	m := completionModel(t)
	m.input.SetValue("/se")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "/sessions " || m.sessions != nil {
		t.Fatal("Tab must complete the command without running it or switching panes")
	}
	m.input.SetValue("/mode p")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "/mode plan " {
		t.Fatalf("mode suggestion = %q", m.input.Value())
	}
	m.input.SetValue("/")
	m.syncSuggestions()
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.suggestions.Selected != 1 {
		t.Fatal("Down must select the next suggestion")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.Update(tickMsg{})
	if len(m.suggestions.Items) != 0 || m.input.Value() != "/" {
		t.Fatal("Esc must dismiss without changing the draft; ticks must not reopen")
	}
	m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if len(m.suggestions.Items) != 1 || m.suggestions.Items[0].Value != "/help" {
		t.Fatal("typing after dismiss should refresh suggestions")
	}
}

func TestEnterRunsSelectedCommand(t *testing.T) {
	for _, prefix := range []string{"/", "/he", "/help"} {
		m := completionModel(t)
		m.input.SetValue(prefix)
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.dialog == nil || !m.dialog.help || m.input.Value() != "" {
			t.Fatalf("Enter on %q must execute selected help", prefix)
		}
		for _, block := range m.transcript.blocks {
			if strings.Contains(block.text, "unknown command") {
				t.Fatalf("literal prefix submitted: %q", block.text)
			}
		}
	}
	m := completionModel(t)
	m.input.SetValue("/")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.sessions == nil || m.dialog != nil {
		t.Fatal("Enter must execute the highlighted Sessions command")
	}
}

func TestEnterCompletesFileWithoutSubmittingAndRespectsTheRunDuringARun(t *testing.T) {
	m := completionModel(t)
	writeReference(t, m, "notes.txt", []byte("hello"))
	m.input.SetValue("read @no")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.input.Value() != "read @notes.txt " || m.state != stDone {
		t.Fatal("Enter on a file suggestion must insert without submitting")
	}
	m.input.SetValue("/new review @no")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.input.Value() != "/new review @notes.txt " || m.state != stDone {
		t.Fatal("file completion inside /new must not execute the command")
	}
	m.input.SetValue("read @notes.txt")
	m.syncSuggestions()
	if len(m.suggestions.Items) != 0 {
		t.Fatal("a complete file path should allow normal submission")
	}
	m.state = stRunning
	// /new acts on the run, so completing it queues rather than starting a
	// second task over the first.
	m.input.SetValue("/ne")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.queued != "/new" || m.dialog != nil {
		t.Fatalf("a run-affecting command must queue during a run: %q", m.queued)
	}

	// /help does not, so the same two keys open it instead. Both paths
	// start from a completed command in the picker; only the declaration
	// differs.
	m.queued = ""
	m.input.SetValue("/he")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.queued != "" || m.dialog == nil {
		t.Fatalf("a view command must run during a run: queued %q, dialog %v",
			m.queued, m.dialog != nil)
	}
}

func TestSuggestionPopupHasBackgroundAndFits(t *testing.T) {
	for _, width := range []int{40, 80, 140} {
		m := completionModel(t)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 18})
		m.input.SetValue("/")
		m.syncSuggestions()
		body := m.suggestionView(m.transcript.Viewport.View())
		if !strings.Contains(body, "╭─ Commands") || !strings.Contains(body, "Enter run") || !strings.Contains(body, "48;") {
			t.Fatalf("missing popup chrome or background: %q", body)
		}
		if lipgloss.Height(body) != m.transcript.Viewport.Height() {
			t.Fatal("popup changed transcript height")
		}
		for _, line := range strings.Split(body, "\n") {
			if lipgloss.Width(line) > m.transcript.Viewport.Width() {
				t.Fatal("popup exceeds transcript width")
			}
		}
	}
}

func TestPopupShowsSixSuggestionsAndUsesBreathingRow(t *testing.T) {
	for _, width := range []int{80, 140} {
		m := completionModel(t)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		before := m.View().Cursor.Y
		m.input.SetValue("/")
		m.syncSuggestions()
		view := m.View()
		lines := strings.Split(view.Content, "\n")
		count, bottom := 0, -1
		for i, line := range lines {
			pane := string([]rune(ansi.Strip(line))[m.promptOffset():])
			if strings.Contains(pane, "/") && strings.HasPrefix(pane, "│") {
				count++
			}
			if strings.Contains(line, "╰") {
				bottom = i
			}
		}
		if count != 12 || bottom < 0 {
			t.Fatalf("%d columns: got %d suggestions", width, count)
		}
		if !strings.Contains(lines[bottom+1], "────") {
			t.Fatal("popup must occupy the former blank row above the divider")
		}
		if len(lines) != m.termH || view.Cursor.Y != before {
			t.Fatal("popup moved the composer or changed frame height")
		}
	}
}

func TestFileSuggestionsBrowseDirectoriesAndQuoteSpaces(t *testing.T) {
	m := completionModel(t)
	writeReference(t, m, "source/my code.go", []byte("package sample"))
	m.input.SetValue("inspect @so")
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "inspect @source/" {
		t.Fatalf("directory = %q", m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != `inspect @"source/my code.go" ` || len(m.suggestions.Items) != 0 {
		t.Fatalf("file completion = %q", m.input.Value())
	}
	text, images, err := splitAttachments(m.ws, m.input.Value())
	if err != nil || len(images) != 0 || !strings.Contains(text, "package sample") {
		t.Fatalf("completion must produce a usable reference: %q %v", text, err)
	}
}

func TestCompletionAtCursorPreservesSuffixAndUnicode(t *testing.T) {
	m := completionModel(t)
	writeReference(t, m, "файл.go", []byte("package sample"))
	m.input.SetValue("first line\nread @ф then explain")
	m.input.SetCursorColumn(len([]rune("read @ф")))
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "first line\nread @файл.go then explain" || m.input.Line() != 1 || m.input.Column() != len([]rune("read @файл.go ")) {
		t.Fatalf("cursor completion lost context: %q, cursor %d:%d", m.input.Value(), m.input.Line(), m.input.Column())
	}
	m.input.SetValue("send me@example.com")
	m.syncSuggestions()
	if len(m.suggestions.Items) != 0 {
		t.Fatal("email must not open a file picker")
	}
}

func TestSuggestionsYieldToOverlaysAndPreserveFrame(t *testing.T) {
	m := completionModel(t)
	m.input.SetValue("/")
	m.syncSuggestions()
	view := m.View()
	if lipgloss.Height(view.Content) != m.termH || view.Cursor == nil {
		t.Fatal("suggestions must preserve screen height and input caret")
	}
	m.openHelp()
	m.syncSuggestions()
	if len(m.suggestions.Items) != 0 {
		t.Fatal("help must hide suggestions")
	}
	m.closeDialog()
	m.state = stRunning
	m.input.SetValue("read @notes.txt")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.queued != "read @notes.txt" {
		t.Fatal("queue must retain original references for resolution when sent")
	}
}

func TestTextReferencesBoundedAndDeduplicated(t *testing.T) {
	m := completionModel(t)
	writeReference(t, m, "notes.txt", []byte("hello\nworld"))
	prompt := "compare\n@notes.txt with @notes.txt"
	text, _, err := splitAttachments(m.ws, prompt)
	if err != nil || !strings.HasPrefix(text, prompt) || strings.Count(text, "hello\nworld") != 1 || displayInputContent(text) != prompt {
		t.Fatalf("text snapshot = %q %v", text, err)
	}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"binary.bin", []byte{'a', 0, 'b'}},
		{"invalid.txt", []byte{0xff}},
		{"large.txt", []byte(strings.Repeat("x", maxReferenceBytes+1))},
	} {
		writeReference(t, m, tc.name, tc.data)
		if _, _, err := splitAttachments(m.ws, "read @"+tc.name); err == nil {
			t.Fatalf("must reject %s", tc.name)
		}
	}
	for _, name := range []string{"../outside.txt", "missing.go", "\"missing file.txt\""} {
		if _, _, err := splitAttachments(m.ws, "read @"+name); err == nil {
			t.Fatalf("must reject reference %s", name)
		}
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeReference(t, m, name, []byte(strings.Repeat("x", maxReferenceBytes)))
	}
	if _, _, err := splitAttachments(m.ws, "read @a.txt @b.txt @c.txt"); err == nil {
		t.Fatal("must enforce aggregate size limit")
	}
}
