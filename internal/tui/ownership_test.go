package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/workspace"
)

func TestOpeningSessionRetainsContextEstimate(t *testing.T) {
	root := t.TempDir()
	writeSessionState(t, root, "chat", userHistory(strings.Repeat("context ", 100)))
	m := sessionsTestModel(t, listSessionsIn(root))
	m.openSelected()
	if m.tokens <= 0 || !m.tokensEst || m.usage.source != "Saved history estimate" {
		t.Fatalf("restored context was reset: %d %+v", m.tokens, m.usage)
	}
}

func TestPastedSearchRecomputesResults(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.wsRoot = t.TempDir()
	root := sessionsRootFor(t, m.wsRoot)
	writeSessionState(t, root, "alpha", userHistory("alpha"))
	writeSessionState(t, root, "beta", userHistory("beta"))
	m.openSessions()
	finishSessionRefresh(t, m)
	m.Update(tea.PasteMsg{Content: "alpha"})
	if m.sessions.filter.Value() != "alpha" || len(m.sessions.entries) != 1 || m.sessions.entries[0].id != "alpha" {
		t.Fatal("paste changed query without filtering")
	}
}

func TestCompletionAndPasteRespectHelpOwner(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stRunning
	m.input.SetValue("unsent draft")
	m.openHelp()
	m.Update(doneMsg{answer: "done"})
	if m.activeInputOwner() != focusDialog || m.input.Focused() {
		t.Fatal("completion focused hidden input")
	}
	m.Update(tea.PasteMsg{Content: "hidden paste"})
	if m.input.Value() != "unsent draft" {
		t.Fatal("Help allowed clipboard into composer")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.input.Focused() {
		t.Fatal("closing Help failed to restore input")
	}
}

func TestImageReferencePreservesMultilinePrompt(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "shot.png"), []byte("image"), 0644); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	text := "Review:\n    if ready:\n        go()\n@shot.png"
	clean, images, err := splitAttachments(ws, text)
	if err != nil || len(images) != 1 || clean != "Review:\n    if ready:\n        go()" {
		t.Fatalf("formatting lost: %q %v %v", clean, images, err)
	}
}

func TestLoadedHistoryDoesNotInventTimestamps(t *testing.T) {
	for _, b := range renderHistory(userHistory("saved")) {
		if !b.at.IsZero() {
			t.Fatal("history was stamped with reopen time")
		}
	}
	if userBlock("live").at.IsZero() {
		t.Fatal("live messages lost their timestamps")
	}
}

func TestViewDoesNotChangeSelectionOrFocus(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.nav.entries = []sessionEntry{{title: "session", updated: time.Now()}}
	m.nav.Cursor, m.nav.Offset = 99, 99
	m.header = headerState{focused: true, index: 99, menu: "model", row: 99}
	beforeHeader := m.header
	beforeFocused := m.input.Focused()
	m.View()
	m.View()
	if !reflect.DeepEqual(m.header, beforeHeader) || m.nav.Cursor != 99 || m.nav.Offset != 99 || m.input.Focused() != beforeFocused {
		t.Fatal("render changed UI state")
	}
	m.header = headerState{}
	m.openHelp()
	m.dialog.offset = 999
	m.View()
	if m.dialog.offset != 999 {
		t.Fatal("Help rendering changed scroll state")
	}
}

func TestMarkdownTablesAlignTerminalCells(t *testing.T) {
	rows := renderTable([]string{"| 名称 | name |", "| --- | --- |", "| 中文 | x |", "| é | y |"})
	width := lipgloss.Width(rows[0].text)
	for _, row := range rows {
		if lipgloss.Width(row.text) != width {
			t.Fatalf("table cell widths differ: %q", row.text)
		}
	}
}

func TestSessionScanRunsOutsideUpdateAndRejectsStaleResults(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.wsRoot = t.TempDir()
	writeSessionState(t, sessionsRootFor(t, m.wsRoot), "one", userHistory("one"))
	m.refreshNavigator()
	first := m.takeCommands()
	if len(m.nav.entries) != 0 {
		t.Fatal("refresh scanned synchronously")
	}
	old := first().(sessionsLoadedMsg)
	m.refreshNavigator()
	_, next := m.Update(old)
	if len(m.nav.entries) != 0 {
		t.Fatal("stale scan replaced current state")
	}
	if next == nil {
		t.Fatal("latest scan command missing")
	}
	m.Update(next())
	if len(m.nav.entries) != 1 {
		t.Fatal("current scan not applied")
	}
}

func TestSessionIndexCachesUnchangedHistoryAndLoadsSearchOnDemand(t *testing.T) {
	root := t.TempDir()
	dir := writeSessionState(t, root, "one", userHistory("original"))
	entries, cache := scanSessions(root, nil, false)
	if len(entries) != 1 || len(entries[0].search) != 0 {
		t.Fatal("sidebar eagerly indexed history")
	}
	entries, cache = scanSessions(root, cache, true)
	if len(entries[0].search) == 0 {
		t.Fatal("search not indexed")
	}
	// Equal metadata must reuse cached text; a deliberately marked value makes this observable.
	saved := cache[dir]
	saved.Preview = "cached marker"
	cache[dir] = saved
	entries, cache = scanSessions(root, cache, true)
	if entries[0].preview != "cached marker" {
		t.Fatal("unchanged history was reloaded")
	}
	if err := workspace.WriteSessionTitle(dir, "renamed"); err != nil {
		t.Fatal(err)
	}
	entries, cache = scanSessions(root, cache, true)
	if entries[0].title != "renamed" {
		t.Fatal("rename not reflected")
	}
	if err := workspace.WriteSessionTitle(dir, ""); err != nil {
		t.Fatal(err)
	}
	entries, _ = scanSessions(root, cache, true)
	if entries[0].title != "original" {
		t.Fatal("cleared title did not return to derived title")
	}
}

func TestSessionCommandDoesNotReadLiveModel(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	root := sessionsRootFor(t, t.TempDir())
	writeSessionState(t, root, "one", userHistory("captured root"))
	m.refreshNavigator()
	cmd := m.takeCommands()
	// Move the session library before executing the worker. It must use
	// the root captured with the command, not the live one.
	tasksRoot = func() string { return filepath.Join(t.TempDir(), "moved") }
	m.sessions = &sessionsState{filter: newSearchInput(80)}
	result := cmd().(sessionsLoadedMsg)
	if result.root != root || len(result.entries) != 1 || len(result.entries[0].search) != 0 {
		t.Fatal("worker read changed UI state")
	}
}

func TestTranscriptCacheInvalidatesChangedBlocksAndGeometry(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.appendBlock(answerBlock("original"))
	original := m.transcript.render.cache[0].text
	m.live = "streamed"
	m.refreshContent()
	if m.transcript.render.cache[0].text != original {
		t.Fatal("unchanged block rerendered differently")
	}
	m.transcript.blocks[0].text = "changed"
	m.refreshContent()
	if !strings.Contains(m.transcript.Viewport.View(), "changed") || m.transcript.render.cache[0].text == original {
		t.Fatal("changed block kept cached text")
	}
	m.width(tea.WindowSizeMsg{Width: 40, Height: 24})
	if m.transcript.render.cache[0].width != 38 {
		t.Fatal("resize did not invalidate cache")
	}
	m.transcript.compact = !m.transcript.compact
	m.refreshContent()
	if m.transcript.render.cache[0].expanded != !m.transcript.compact {
		t.Fatal("details toggle did not invalidate cache")
	}
}

func TestCompletionClosesCancelledPermissionOverlay(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stPermission
	m.pushOverlay()
	m.Update(doneMsg{})
	if len(m.overlays) != 0 || m.activeInputOwner() != focusInput {
		t.Fatal("finished gate retained overlay ownership")
	}
}

func TestHelpRestoresNavigatorOwner(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.width(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.activeInputOwner() != focusNavigator || !m.navFocused || m.input.Focused() {
		t.Fatal("Help lost Sessions ownership")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.activeInputOwner() != focusInput || !m.input.Focused() {
		t.Fatal("hidden sidebar retained ownership after resize")
	}
}

func TestHistoryMatchTranslationSkipsReasoningAndToolCards(t *testing.T) {
	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "question"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "lookup", Name: "read_file"}}},
		{Role: llm.RoleTool, ToolCallID: "lookup", Content: "result"},
		{Role: llm.RoleAssistant, Content: "<think>private</think> answer"},
	}
	index := historyMatchBlock(history, 4)
	blocks := renderHistory(history)
	if index >= len(blocks) || blocks[index].role != roleAnswer || blocks[index].text != "answer" {
		t.Fatalf("search landed on wrong block: %d %+v", index, blocks)
	}
}
