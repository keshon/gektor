package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/keshon/tars/internal/tui/components/selection"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/session"
	"github.com/keshon/tars/internal/tars"
	"github.com/keshon/tars/internal/workspace"
)

// Session search: a centered popup over the transcript with open,
// rename and delete. Each task directory holds the whole turn
// history in state.json, so one row per directory.
//
// Storage follows pi, not an index: the title lives in the session dir
// (written once at creation, rewritten on rename), the screen scans on
// every open. No second source of truth means nothing to drift, no
// per-step write-path cost, and delete removes the title with the dir.
// Delete unloads the active session at rest; rename allows it (the dir, and
// every -resume path, is unchanged).

type sessionsMode int

const (
	sessList sessionsMode = iota
	// sessRename borrows the reject-note input: single line, capped,
	// esc-backed. Enter commits, like the gate stager.
	sessRename
	// sessConfirm is a y/n delete confirmation over the selected row.
	sessConfirm
)

type sessionEntry struct {
	id           string
	title        string
	dir          string
	msgs         int
	updated      time.Time
	status       string
	mode         string
	preview      string
	search       []sessionText
	matchText    string
	matchMessage int
	historyMatch bool
}

type sessionsState struct {
	loading bool
	entries []sessionEntry
	all     []sessionEntry
	filter  textinput.Model
	selection.Model
	// offset is the first visible row: the list scrolls under a
	// stationary viewport instead of clipping past it.
	mode  sessionsMode
	flash string
	// root is the tasks dir scanned at open; mutations re-scan it so
	// the list always reflects disk, never a stale copy.
	root          string
	fromNavigator bool
}

// tasksRoot is where saved sessions live, under the user's TARS
// directory. A var rather than a bare call so tests can point it
// somewhere temporary instead of writing to the real home directory.
var tasksRoot = func() string { return tars.Sub("tasks") }

// sessChrome counts border title, search, gap, hint and bottom border,
// plus mutation prompts. Each result has a title and an excerpt.
func (m *model) sessChrome() int {
	chrome := 5
	if m.sessions.flash != "" {
		chrome++
	}
	if m.sessions.mode == sessRename {
		chrome += 2
	} else if m.sessions.mode == sessConfirm {
		chrome++
	}
	return chrome
}

// sessVisible is how many entries fit the viewport. Stateless math
// shared by keys and render, so paging and clipping never disagree.
func (m *model) sessVisible() int {
	availH := m.termH - 3
	if m.ready {
		availH = m.paneHeight()
	}
	if n := min(6, (availH-m.sessChrome())/2); n > 0 {
		return n
	}
	return 1
}

// clampOffset keeps the cursor on screen: follow down, pull up,
// pin to bounds after delete re-scans.
func (m *model) clampOffset() {
	m.sessions.clamp(m.sessVisible())
}

// ageString renders mtime as "5m ago"; zero time (no snapshot yet)
// renders a dash, never "0s ago".
func ageString(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// openSessions builds the list and pushes the overlay; closeSessions
// tears it down with focus restored. Balanced by construction: every
// open pairs with esc/enter or run start.
func (m *model) openSessions() {
	filter := newSearchInput(max(m.termW-14, 1))
	filter.Focus()
	m.sessions = &sessionsState{entries: m.nav.entries, all: m.nav.entries, root: m.sessionRoot(), filter: filter}
	m.refreshNavigator()
	m.pushOverlay()
	// Search overlays the existing panes without changing composer geometry.
	m.fitBottom()
	m.refreshContent()
}

func (m *model) closeSessions() {
	m.sessions = nil
	m.resetNote()
	m.popOverlay()
	m.fitBottom()
	m.refreshContent()
	if m.transcript.Follow {
		m.transcript.Viewport.GotoBottom()
	}
}

// resetNote hands the borrowed input back with its own prompt and
// limit, or the next gate asks "name> ".
func (m *model) resetNote() {
	m.note.Blur()
	m.note.SetValue("")
	m.note.Prompt = "note> "
	m.note.CharLimit = 240
}

// sessionsKey handles keys while the screen is open. Letters are
// Search accepts typing; ctrl+r/ctrl+d are list commands — except rename mode, where the
// borrowed note input owns every key but esc/enter, like gsReject.
func (m *model) sessionsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	defer m.fitBottom()
	s := m.sessions
	if msg.String() == "ctrl+c" || msg.String() == "ctrl+q" {
		m.quit = true
		m.cancel()
		return m, tea.Quit
	}
	if s.mode == sessRename {
		switch msg.String() {
		case "enter":
			m.commitRename()
			return m, nil
		case "esc":
			m.resetNote()
			s.mode = sessList
			if s.fromNavigator {
				m.closeSessions()
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.note, cmd = m.note.Update(msg)
		return m, cmd
	}
	if s.mode == sessConfirm {
		switch msg.String() {
		case "y", "Y":
			m.deleteSelected()
			return m, nil
		case "n", "N", "esc":
			s.mode = sessList
			s.flash = ""
			if s.fromNavigator {
				m.closeSessions()
			}
			return m, nil
		}
		return m, nil
	}
	key := msg.String()
	switch key {
	case "esc", "ctrl+p":
		m.closeSessions()
		return m, nil
	case "enter":
		return m, m.openSelected()
	case "up", "down":
		s.flash = ""
		if len(s.entries) == 0 {
			return m, nil
		}
		if msg.String() == "up" && s.Cursor > 0 {
			s.Cursor--
		}
		if msg.String() == "down" && s.Cursor < len(s.entries)-1 {
			s.Cursor++
		}
		m.clampOffset()
		return m, nil
	case "pgup", "pgdown":
		s.flash = ""
		if len(s.entries) == 0 {
			return m, nil
		}
		step := m.sessVisible()
		if msg.String() == "pgup" {
			s.Cursor -= step
			if s.Cursor < 0 {
				s.Cursor = 0
			}
		} else {
			s.Cursor += step
			if s.Cursor > len(s.entries)-1 {
				s.Cursor = len(s.entries) - 1
			}
		}
		m.clampOffset()
		return m, nil
	case "ctrl+r":
		if len(s.entries) == 0 {
			return m, nil
		}
		s.flash = ""
		s.mode = sessRename
		m.note.Prompt = "name> "
		m.note.CharLimit = 80
		m.note.SetValue("")
		return m, m.note.Focus()
	case "ctrl+d":
		if len(s.entries) == 0 {
			return m, nil
		}
		s.flash = ""
		s.mode = sessConfirm
		return m, nil
	}
	if m.activeInputOwner() == focusSearch {
		return m.updateSearchInput(msg)
	}

	return m, nil
}

// openSelected switches to the selected session: snapshot loads,
// transcript rebuilds from history, follow-ups continue the
// conversation via the normal Resume path. Only at rest: switching
// mid-run would orphan the in-flight turn.
func (m *model) openSelected() tea.Cmd {
	s := m.sessions
	if len(s.entries) == 0 {
		return nil
	}
	if m.state != stDone {
		s.flash = "stop the run first"
		return nil
	}
	cur := s.entries[s.Cursor]
	if m.application != nil {
		if cmd, handled := m.openApplicationSession(cur); handled {
			return cmd
		}
	}
	if cur.mode == "Mission" {
		s.flash = "Mission: run agent -tui -resume " + cur.dir
		return nil
	}
	stateFile := filepath.Join(cur.dir, "state.json")
	history, err := session.ReadHistory(stateFile)
	if err != nil {
		s.flash = "Cannot read saved history: " + err.Error()
		return nil
	}
	task := session.TitleFor(history)
	if task == "" {
		task = cur.title
	}
	if m.newSession == nil {
		s.flash = "sessions unavailable here"
		return nil
	}
	oldPlan := m.plan
	if cur.mode == "Plan" {
		m.plan = true
	} else if cur.mode == "Act" {
		m.plan = false
	}
	sess, err := m.newSession(task, stateFile, nil)
	if err != nil {
		m.plan = oldPlan
		s.flash = "cannot open session: " + err.Error()
		return nil
	}
	m.saveDraft()
	m.sess = sess
	m.stateFile = stateFile
	m.sessionTitle = cur.title
	m.mission = false
	m.resetRunFacts()
	m.restoreDraft()
	blocks, boundary := renderHistoryAt(history)
	m.transcript.blocks = append([]block{markerBlock("- opened " + cur.title + " -")}, blocks...)
	m.restoreContext(history)
	m.retryRun = nil
	m.runErr = nil
	m.answer = ""
	m.steps = 0
	m.state = stDone
	m.navFocused = false
	m.closeSessions()
	m.fitBottom()
	m.refreshContent()
	// Land at the top: a stale offset into replaced blocks shows
	// mid-history or blank. follow stays on so live turns behave.
	m.transcript.Follow = true
	m.transcript.Viewport.GotoTop()
	if cur.historyMatch {
		// Match indices refer to renderHistory; the opened marker adds one block.
		target := historyMatchBlock(blocks, boundary, cur.matchMessage) + 1
		row := m.transcript.PartOffset(target)
		m.transcript.Follow = false
		m.transcript.Viewport.SetYOffset(row)
	}
	m.restoreDraft()
	return m.input.Focus()
}

// renderHistory rebuilds the transcript from a loaded snapshot so a
// resumed or compacted session reads like it was never left. System
// prompts stay out; loop nudges retain their [harness] provenance as
// system notices. Tool cards re-attach their results by
// call ID, same pairing the live path uses. No markers: the caller
// prepends its own (resumed/compacted), this renders conversation.
//
// boundary[i] is the block index where message i starts, so a caller that
// needs to point at one source message can do it without rendering the
// prefix a second time and throwing it away.
func renderHistory(history []llm.Message) []block {
	blocks, _ := renderHistoryAt(history)
	return blocks
}

func renderHistoryAt(history []llm.Message) ([]block, []int) {
	var out []block
	boundary := make([]int, len(history)+1)
	for i, msg := range history {
		boundary[i] = len(out)
		switch msg.Role {
		case llm.RoleSystem:
			continue
		case llm.RoleUser:
			if strings.HasPrefix(msg.Content, "[harness] ") {
				out = append(out, markerBlock(msg.Content))
				continue
			}
			if strings.TrimSpace(msg.Content) == "" && len(msg.Images) == 0 {
				continue
			}
			out = append(out, userBlock(displayInputContent(msg.Content)+imageSuffix(msg.Images)))
		case llm.RoleAssistant:
			out = append(out, assistantBlocks(msg.Content, msg.Reasoning)...)
			for _, tc := range msg.ToolCalls {
				out = append(out, toolCardBlock(tc.ID, tc.Name+" "+truncate(string(tc.Arguments), 120)))
			}
		case llm.RoleTool:
			attached := false
			if msg.ToolCallID != "" {
				for i := len(out) - 1; i >= 0; i-- {
					b := &out[i]
					if b.role == roleTool && b.open && b.callID == msg.ToolCallID {
						b.result = msg.Content
						b.open = false
						b.failed = strings.HasPrefix(msg.Content, "error:")
						attached = true
						break
					}
				}
			}
			if !attached {
				out = append(out, resultBlock(msg.Content))
			}
		}
	}
	// Saved histories do not store message times; do not invent them on reopen.
	for i := range out {
		out[i].at = time.Time{}
	}
	boundary[len(history)] = len(out)
	return out, boundary
}

// commitRename stores the title beside the session (empty restores the
// derived text) and refreshes the row in place: order never moves
// under the cursor, only the title changes.
func (m *model) commitRename() {
	s := m.sessions
	if len(s.entries) == 0 {
		s.mode = sessList
		return
	}
	name := workspace.TitleLine(m.note.Value())
	m.resetNote()
	s.mode = sessList
	cur := &s.entries[s.Cursor]
	if name == "" {
		// Clearing removes the stored title: the row falls back to
		// the derived text live, tracking history instead of a stale
		// copy of it.
		load := func() ([]llm.Message, error) {
			return session.ReadHistory(filepath.Join(cur.dir, "state.json"))
		}
		if history, err := load(); err == nil {
			name = session.TitleFor(history)
		}
		if name == "" {
			name = cur.id
		}
		if err := workspace.WriteSessionTitle(cur.dir, ""); err != nil {
			s.flash = "rename failed: " + err.Error()
			return
		}
	} else if err := workspace.WriteSessionTitle(cur.dir, name); err != nil {
		s.flash = "rename failed: " + err.Error()
		return
	}
	s.flash = ""
	cur.title = name
	m.updateSessionTitle(cur.dir, name)
	m.refreshNavigator()
	if s.all != nil {
		s.replaceEntries(s.all)
	}
	if s.fromNavigator {
		m.closeSessions()
	}
}

// deleteSelected removes the session dir — title goes with it, so no
// orphan rows — then re-scans. Deleting the active session unloads it
// first (the empty state, then the dir is just files). Only at rest:
// unloading mid-run would orphan the in-flight turn.
func (m *model) deleteSelected() {
	s := m.sessions
	if len(s.entries) == 0 {
		s.mode = sessList
		return
	}
	cur := s.entries[s.Cursor]
	if m.stateFile != "" && sameSession(cur.dir, m.stateFile) {
		if m.state != stDone {
			s.mode = sessList
			s.flash = "stop the run first"
			return
		}
		m.newTask()
	}
	target, err := filepath.Abs(cur.dir)
	root, rootErr := filepath.Abs(s.root)
	if err != nil || rootErr != nil || !strings.EqualFold(filepath.Dir(target), root) {
		s.mode, s.flash = sessList, "refusing to delete outside the session directory"
		return
	}
	if err := os.RemoveAll(target); err != nil {
		s.mode = sessList
		s.flash = "delete failed: " + err.Error()
		return
	}
	delete(m.drafts, filepath.Join(cur.dir, "state.json"))
	m.nav.entries = withoutSession(m.nav.entries, cur.dir)
	s.all = withoutSession(s.all, cur.dir)
	s.entries = withoutSession(s.entries, cur.dir)
	m.refreshNavigator()

	if s.Cursor >= len(s.entries) && s.Cursor > 0 {
		s.Cursor--
	}
	m.clampOffset()
	s.mode = sessList
	s.flash = ""
	if s.fromNavigator {
		m.closeSessions()
	}
}

// headerLine renders the static brand and session title beside the clock.
// During a run the clock shows elapsed time; activity lives in the badge strip.

// activeSessionTitle names the open session for the header and the
// sessions bottom bar: stored title, then dir id; a dash while a session is open
// without one yet. No history reads: the header renders every second.
func (m *model) activeSessionTitle() string {
	if m.stateFile == "" {
		return "New session"
	}
	dir := filepath.Dir(m.stateFile)
	for _, e := range m.nav.entries {
		if sameSession(e.dir, m.stateFile) {
			return e.title
		}
	}
	if m.sessionTitle != "" {
		return m.sessionTitle
	}
	return filepath.Base(dir)
}

// sessionRoot follows the chosen workspace, independently of the process cwd.
func (m *model) sessionRoot() string {
	if m.application != nil && m.config != nil {
		return m.config.Directory
	}
	// Saved sessions belong to the user, not to the workspace: the same
	// list has to appear whichever directory TARS was pointed at.
	return tasksRoot()
}
func sameSession(dir, stateFile string) bool {
	if stateFile == "" {
		return false
	}
	a, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	b, err := filepath.Abs(filepath.Dir(stateFile))
	return err == nil && strings.EqualFold(a, b)
}

func (s *sessionsState) clamp(visible int) {
	s.Clamp(len(s.entries), visible)
}

// applyFilter narrows the list to the sessions matching the query, title
// first and history second.
//
// The searchable corpus is folded once at index time and kept on the entry.
// Folding it here instead cost one allocation per cached message per
// keystroke, over the whole conversation text of every session.
func (s *sessionsState) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(s.filter.Value()))
	var titles, history []sessionEntry
	for _, original := range s.all {
		e := original
		e.historyMatch, e.matchText = false, ""
		if query == "" || strings.Contains(strings.ToLower(e.title+" "+e.id), query) {
			if query != "" {
				for _, message := range e.search {
					if strings.Contains(message.lower, query) {
						e.matchText = message.text
						break
					}
				}
			}
			titles = append(titles, e)
			continue
		}
		for _, message := range e.search {
			if strings.Contains(message.lower, query) {
				e.matchText, e.matchMessage, e.historyMatch = message.text, message.message, true
				history = append(history, e)
				break
			}
		}
	}
	s.entries = append(titles, history...)
	s.Cursor, s.Offset = 0, 0
}

func (s *sessionsState) replaceEntries(entries []sessionEntry) {
	index, selected := s.Cursor, ""
	if len(s.entries) > 0 {
		selected = s.entries[s.Cursor].dir
	}
	s.all = entries
	s.applyFilter()
	s.Cursor = min(index, max(len(s.entries)-1, 0))
	for i, e := range s.entries {
		if e.dir == selected {
			s.Cursor = i
			break
		}
	}
}

func (m *model) browserMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	s := m.sessions
	if s.mode != sessList {
		return m, nil
	}
	g := m.searchGeometry()
	mouse := msg.Mouse()
	if mouse.X < g.x || mouse.X >= g.x+g.width || mouse.Y < g.y || mouse.Y >= g.y+g.height {
		return m, nil
	}
	switch mouse.Button {
	case tea.MouseWheelUp:
		s.Cursor--
	case tea.MouseWheelDown:
		s.Cursor++
	case tea.MouseLeft:
		if !isMouseClick(msg) {
			return m, nil
		}
		row := (mouse.Y - g.y - 3) / 2
		if mouse.Y < g.y+3 || row >= m.sessVisible() || s.Offset+row >= len(s.entries) {
			return m, nil
		}
		s.Cursor = s.Offset + row
	default:
		return m, nil
	}
	m.clampOffset()
	return m, nil
}

func (m *model) updateSearchInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	s := m.sessions
	before := s.filter.Value()
	var cmd tea.Cmd
	s.filter, cmd = s.filter.Update(msg)
	if s.filter.Value() != before {
		s.applyFilter()
	}
	return m, cmd
}

func newSearchInput(width int) textinput.Model {
	filter := textinput.New()
	filter.SetStyles(textInputStyles(filter.Styles()))
	filter.Prompt = "> "
	filter.SetVirtualCursor(false)
	style := filter.Styles()
	style.Cursor.Shape = tea.CursorUnderline
	filter.SetStyles(style)
	filter.Placeholder = "Session name or history text"
	filter.CharLimit = 80
	filter.SetWidth(width)
	return filter
}

func withoutSession(entries []sessionEntry, dir string) []sessionEntry {
	out := make([]sessionEntry, 0, len(entries))
	for _, e := range entries {
		if e.dir != dir {
			out = append(out, e)
		}
	}
	return out
}
func (m *model) updateSessionTitle(dir, title string) {
	for i := range m.nav.entries {
		if m.nav.entries[i].dir == dir {
			m.nav.entries[i].title = title
		}
	}
	if m.sessions != nil {
		for i := range m.sessions.all {
			if m.sessions.all[i].dir == dir {
				m.sessions.all[i].title = title
			}
		}
	}
	if sameSession(dir, m.stateFile) {
		m.sessionTitle = title
	}
}

// Translate a source-message match only when opening its transcript. Indexing
// remains independent of reasoning blocks, tool cards, and display layout.
//
// It reads the block index out of the render the caller already has. The
// previous version rendered every block in the prefix purely to count them
// and threw them away, which doubled the work of opening a session from a
// search hit.
func historyMatchBlock(blocks []block, boundary []int, message int) int {
	message = min(max(message, 0), len(boundary)-1)
	prefix := boundary[message]
	if message >= len(boundary)-1 {
		return prefix
	}
	for i := prefix; i < boundary[message+1]; i++ {
		if blocks[i].role == roleUser || blocks[i].role == roleAnswer {
			return i
		}
	}
	return prefix
}
