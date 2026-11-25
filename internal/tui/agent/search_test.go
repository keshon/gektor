package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/harness"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/pkg/tui/screen"
)

func TestSearchIndexesConversationAndRanksTitles(t *testing.T) {
	root := sessionsRootFor(t, t.TempDir())
	title := writeSessionState(t, root, "title", userHistory("ПАРСЕР implementation"))
	history := writeSessionState(t, root, "history", []llm.Message{
		{Role: llm.RoleUser, Content: "Unrelated title"},
		{Role: llm.RoleAssistant, Content: "<think>private needle</think>\nThe парсер handles UTF-8."},
		{Role: llm.RoleUser, Content: "[harness] hidden needle"},
		{Role: llm.RoleTool, Content: "tool needle"},
		{Role: llm.RoleSystem, Content: "system needle"},
	})
	entries := listSessionsIn(root)
	s := &sessionsState{all: entries}
	s.filter = newNote()
	s.filter.SetValue("парсер")
	s.applyFilter()
	if len(s.entries) != 2 || s.entries[0].dir != title || s.entries[1].dir != history || !s.entries[1].historyMatch {
		t.Fatalf("title ranking or Unicode history match failed: %+v", s.entries)
	}
	for _, query := range []string{"private needle", "hidden needle", "tool needle", "system needle"} {
		s.filter.SetValue(query)
		s.applyFilter()
		if len(s.entries) != 0 {
			t.Fatalf("searched hidden content: %s", query)
		}
	}
	s.filter.SetValue("")
	s.applyFilter()
	if len(s.entries) != len(entries) {
		t.Fatal("empty query lost recent sessions")
	}
}

func TestSearchPopupGeometryAndDraftPreservation(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {120, 30}, {80, 24}, {40, 18}, {20, 10}} {
		m := testModel()
		m.state = stDone
		m.wsRoot = t.TempDir()
		m.sessionsRoot = sessionsRootFor(t, m.wsRoot)
		for i := 0; i < 9; i++ {
			writeSessionState(t, m.sessionsRoot, fmt.Sprint(i), userHistory(fmt.Sprintf("Session %d", i)))
		}
		m.testUpdate(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.input.SetValue("draft\nsecond line")
		m.fitBottom()
		m.openSessions()
		finishSessionRefresh(t, m)
		m.sessions.filter.SetValue("Session")
		m.sessions.applyFilter()
		g := m.searchGeometry()
		view := m.testView()
		if strings.Contains(m.sessions.filter.View(), "…") {
			t.Fatal("search field clipped its own padding")
		}
		if view.Cursor == nil || view.Cursor.X < g.x || view.Cursor.X >= g.x+g.width || view.Cursor.Y != g.y+2 {
			t.Fatalf("size %v: caret %+v geometry %+v", size, view.Cursor, g)
		}
		for _, line := range strings.Split(view.Content, "\n") {
			if lipgloss.Width(line) != size[0] {
				t.Fatalf("size %v overflow", size)
			}
		}
		if lipgloss.Height(view.Content) != size[1] || g.x != (size[0]-g.width)/2 {
			t.Fatal("popup not centered or frame clipped")
		}
		if m.sessVisible()*4+m.sessChrome() > m.transcript.Viewport.Height() && m.sessVisible() > 1 {
			t.Fatal("search rows do not fit the transcript")
		}
		m.sessionsKey(tea.KeyPressMsg{Code: tea.KeyEsc})
		if m.input.Value() != "draft\nsecond line" {
			t.Fatal("closing search lost draft")
		}
	}
}

func TestSessionBrowserUsesSharedUppercaseFooter(t *testing.T) {
	m := sizeModel(t, testModel())
	m.sessions = &sessionsState{filter: newSearchInput(50), entries: []sessionEntry{{id: "a", title: "First session"}, {id: "b", title: "Second session"}}}
	m.fitBottom()
	text := ansi.Strip(m.searchView())
	for _, want := range []string{"Open session", "ESC close", "ENTER open", "↑↓ select", "1/2"} {
		if !strings.Contains(text, want) {
			t.Fatal("session browser lost shared chrome or count", want, text)
		}
	}
	m.sessions.mode = sessRename
	text = ansi.Strip(m.searchView())
	if !strings.Contains(text, "ENTER save") || strings.Contains(text, "ENTER open") {
		t.Fatal("session footer describes the wrong action", text)
	}
}

func TestSearchHistoryMatchOpensAtMessage(t *testing.T) {
	m := testModel()
	m.state = stDone
	m.wsRoot = t.TempDir()
	m.sessionsRoot = sessionsRootFor(t, m.wsRoot)
	history := userHistory("Unrelated title")
	for i := 0; i < 30; i++ {
		history = append(history, llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("Earlier message %d", i)})
	}
	history = append(history, llm.Message{Role: llm.RoleUser, Content: "Find this unique phrase"})
	for i := 0; i < 20; i++ {
		history = append(history, llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("Later message %d", i)})
	}
	dir := writeSessionState(t, m.sessionsRoot, "match", history)
	assignSavedWorkspace(t, filepath.Join(dir, "state.json"), m.wsRoot)
	m.newSession = func(string, string, []string) (*harness.Session, error) { return &harness.Session{}, nil }
	m.testUpdate(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.openSessions()
	finishSessionRefresh(t, m)
	m.sessions.filter.SetValue("unique phrase")
	m.sessions.applyFilter()
	m.openSelected()
	if m.sessions != nil || m.stateFile != filepath.Join(dir, "state.json") || m.transcript.Follow || m.transcript.Viewport.YOffset() == 0 || !strings.Contains(ansi.Strip(m.transcript.Viewport.View()), "Find this unique phrase") {
		t.Fatal("opening history result did not reveal matching message")
	}
}

func TestSessionRenameAndDeleteKeepModalOpen(t *testing.T) {
	m := testModel()
	m.state = stDone
	m.wsRoot = t.TempDir()
	m.sessionsRoot = sessionsRootFor(t, m.wsRoot)
	dir := writeSessionState(t, m.sessionsRoot, "one", userHistory("Original title"))
	m.testUpdate(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.refreshSessions()
	finishSessionRefresh(t, m)
	m.openSessions()
	finishSessionRefresh(t, m)
	m.input.Blur()
	m.sessionsKey(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.sessions == nil || m.sessions.mode != sessRename {
		t.Fatal("pane rename shortcut missing")
	}
	m.sessions.rename.SetValue("Renamed")
	m.sessionsKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.sessions == nil || m.sessions.mode != sessList || conversation.ReadSessionTitle(dir) != "Renamed" {
		t.Fatal("rename did not return to pane")
	}
	m.sessionsKey(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if m.sessions == nil || m.chrome.DialogInfo().Kind != screen.FormDialog {
		t.Fatal("pane delete shortcut missing")
	}
	confirmation := ansi.Strip(m.testView().Content)
	if !strings.Contains(confirmation, `"Renamed"`) || strings.Contains(confirmation, "[Ins]") {
		t.Fatal("single-session confirmation did not quote the title or hide the picker")
	}
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.sessions == nil || m.sessions.mode != sessList {
		t.Fatal("cancel deletion did not return to pane")
	}
	m.sessionsKey(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	confirmSessionDelete(t, m)
	if _, err := os.Stat(dir); !os.IsNotExist(err) || m.sessions == nil || m.sessions.mode != sessList {
		t.Fatal("confirmed deletion did not remove session and restore pane")
	}

}

func TestSearchHighlightExcerptKeepsUnicode(t *testing.T) {
	text := strings.Repeat("Earlier text ", 20) + "Нужный парсер работает" + strings.Repeat(" later text", 20)
	re := searchRegex("ПАРСЕР")
	excerpt := searchExcerpt(text, re, 60)
	if !strings.Contains(excerpt, "парсер") || lipgloss.Width(excerpt) > 60 {
		t.Fatal("matching excerpt lost Unicode or overflows")
	}
	normal, _, _ := popupStyles()
	if !strings.Contains(highlightSearch(excerpt, re, normal), "38;2;243;213;138") {
		t.Fatal("matching words not highlighted")
	}
	if searchRegex("") != nil {
		t.Fatal("an empty query should not compile a matcher")
	}
}

func TestModernSessionButtonsSaveAndCancelFromVisibleRows(t *testing.T) {
	defer screen.SetModern(false)
	m := sizeModel(t, testModel())
	m.applyAppearance(config.Appearance{Style: "modern", Frame: "minimal"})
	m.wsRoot = t.TempDir()
	m.sessionsRoot = sessionsRootFor(t, m.wsRoot)
	dir := writeSessionState(t, m.sessionsRoot, "one", userHistory("Original title"))
	m.openSessions()
	finishSessionRefresh(t, m)

	click := func(label string) {
		t.Helper()
		rows := strings.Split(ansi.Strip(m.testView().Content), "\n")
		for y, row := range rows {
			x := strings.Index(row, " "+label+" ")
			if x >= 0 && strings.Contains(row, "Cancel") {
				m.testUpdate(tea.MouseClickMsg{X: ansi.StringWidth(row[:x]) + 1, Y: y, Button: tea.MouseLeft})
				return
			}
		}
		t.Fatalf("missing %s button", label)
	}

	m.testUpdate(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m.sessions.rename.SetValue("Renamed")
	click("Save")
	if m.sessions.mode != sessList || conversation.ReadSessionTitle(dir) != "Renamed" {
		t.Fatal("Save did not rename the session and return to the list")
	}
	m.testUpdate(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	click("Cancel")
	if m.sessions.mode != sessList || conversation.ReadSessionTitle(dir) != "Renamed" {
		t.Fatal("Cancel did not preserve the session and return to the list")
	}
}

func TestSessionPinPersistsAndSortsBeforeNewerSessions(t *testing.T) {
	m := sizeModel(t, testModel())
	m.sessionsRoot = sessionsRootFor(t, t.TempDir())
	older := writeSessionState(t, m.sessionsRoot, "older", userHistory("Older note"))
	newer := writeSessionState(t, m.sessionsRoot, "newer", userHistory("Newer note"))
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(older, "state.json"), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	m.openSessions()
	finishSessionRefresh(t, m)
	if m.sessions.entries[0].dir != newer {
		t.Fatal("newest session did not start first")
	}
	m.closeSessions()
	m.stateFile = filepath.Join(older, "state.json")
	if !strings.Contains(sessionFavoriteMenuLabel(m), "Add to favorites") {
		t.Fatal("the Session menu did not offer to add the current session")
	}
	m.activateMenuAction("favorite-current")
	m.openSessions()
	finishSessionRefresh(t, m)
	if first := m.sessions.entries[0]; first.dir != older || !first.pinned {
		t.Fatal("pin did not immediately move the session first")
	}

	fresh := sizeModel(t, testModel())
	fresh.sessionsRoot = m.sessionsRoot
	fresh.openSessions()
	finishSessionRefresh(t, fresh)
	if first := fresh.sessions.entries[0]; first.dir != older || !first.pinned {
		t.Fatal("pin did not survive reloading the sessions")
	}
	fresh.closeSessions()
	fresh.stateFile = filepath.Join(older, "state.json")
	if !strings.Contains(sessionFavoriteMenuLabel(fresh), "Remove from favorites") {
		t.Fatal("the Session menu did not offer to remove the favorite")
	}
	fresh.activateMenuAction("favorite-current")
	fresh.openSessions()
	finishSessionRefresh(t, fresh)
	if first := fresh.sessions.entries[0]; first.dir != newer || sessionPinned(older) {
		t.Fatal("unpin did not restore recency order")
	}
}

func sessionFavoriteMenuLabel(m *Model) string {
	for _, item := range m.menuItems("session") {
		if item.Command == "favorite-current" && item.Enabled {
			return item.Text
		}
	}
	return ""
}

func TestDeletePickerInsertAdvancesAndKeypadPlusMarksAll(t *testing.T) {
	m := sizeModel(t, testModel())
	m.state = stDone
	m.sessionsRoot = sessionsRootFor(t, t.TempDir())
	writeSessionState(t, m.sessionsRoot, "first", userHistory("First note"))
	writeSessionState(t, m.sessionsRoot, "second", userHistory("Second note"))
	m.activateMenuAction("delete-sessions")
	finishSessionRefresh(t, m)
	s := m.sessions
	if s == nil || !s.deleteOnly || !strings.Contains(ansi.Strip(m.testView().Content), "Delete sessions") {
		t.Fatal("Session menu did not open the deletion picker")
	}
	first := s.entries[0].dir
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyInsert})
	if !s.marked[first] || s.Cursor != 1 {
		t.Fatal("Insert did not mark the row and move down")
	}
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyKpPlus})
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyKpPlus})
	if len(s.marked) != 2 {
		t.Fatal("keypad plus did not keep all rows marked")
	}

	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyDelete})
	view := ansi.Strip(m.testView().Content)
	if !strings.Contains(view, "Delete 2 sessions?") || strings.Contains(view, "[Ins]") {
		t.Fatal("confirmation did not replace the deletion picker")
	}
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.sessions != s || len(s.marked) != 2 || !strings.Contains(ansi.Strip(m.testView().Content), "[Ins]") {
		t.Fatal("cancel did not restore the marked picker")
	}
}

func TestSessionBatchDeleteKeepsMarksAcrossSearches(t *testing.T) {
	m := sizeModel(t, testModel())
	m.sessionsRoot = sessionsRootFor(t, t.TempDir())
	first := writeSessionState(t, m.sessionsRoot, "alpha", userHistory("Alpha note"))
	second := writeSessionState(t, m.sessionsRoot, "beta", userHistory("Beta note"))
	keep := writeSessionState(t, m.sessionsRoot, "gamma", userHistory("Keep this note"))
	m.openSessions()
	finishSessionRefresh(t, m)

	s := m.sessions
	s.filter.SetValue("alpha")
	s.applyFilter()
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyInsert})
	if !s.marked[first] {
		t.Fatal("Insert did not mark the current result")
	}
	g := m.searchGeometry()
	markX := g.x + 2 + lipgloss.Width(sessionDeleteLabel+screen.Separator())
	m.testUpdate(tea.MouseClickMsg{X: markX, Y: g.y + g.height - 2, Button: tea.MouseLeft})
	if s.marked[first] {
		t.Fatal("the Insert button did not clear the current mark")
	}
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyKpInsert})
	s.filter.SetValue("beta")
	s.applyFilter()
	m.testUpdate(tea.KeyPressMsg{Code: '+', Text: "+"})
	if s.filter.Value() != "beta" {
		t.Fatal("plain keypad plus was inserted into the search")
	}
	if !s.marked[first] || !s.marked[second] || len(s.marked) != 2 {
		t.Fatal("marking a filtered row changed the hidden mark")
	}
	m.testUpdate(tea.KeyPressMsg{Code: '-', Text: "-"})
	if s.filter.Value() != "beta" || !s.marked[first] || len(s.marked) != 1 {
		t.Fatal("minus did not clear only the matching mark")
	}
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyKpPlus})

	g = m.searchGeometry()
	m.testUpdate(tea.MouseClickMsg{X: g.x + 2, Y: g.y + g.height - 2, Button: tea.MouseLeft})
	if m.chrome.DialogInfo().Kind != screen.FormDialog || !strings.Contains(ansi.Strip(m.testView().Content), "Delete 2 sessions?") {
		t.Fatal("batch confirmation did not show the full marked count")
	}
	m.testUpdate(tea.KeyPressMsg{Code: tea.KeyEsc})
	if len(s.marked) != 2 {
		t.Fatal("cancelling cleared the marks")
	}
	for _, dir := range []string{first, second} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatal(err)
		}
	}

	m.testUpdate(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	confirmSessionDelete(t, m)
	for _, dir := range []string{first, second} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("marked session remains: %s, %v", dir, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal(err)
	}
	if m.stateFile != "" || len(s.marked) != 0 {
		t.Fatal("deletion opened a session or kept deleted marks")
	}
}
