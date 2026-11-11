package agent

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/selection"
)

// The session browser owns search, selection, and its rename input.
// Disk mutations and active-session changes are coordinated by Model.
type sessionsMode int

const (
	sessList sessionsMode = iota
	// sessRename edits the selected title in the browser-owned input.
	sessRename
	// sessConfirm is a y/n delete confirmation over the selected row.
	sessConfirm
	// sessReassign confirms replacing the saved workspace with the current one.
	sessReassign
)

type sessionEntry struct {
	workspace    string
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
	currentWorkspace string
	currentOnly      bool
	loading          bool
	entries          []sessionEntry
	all              []sessionEntry
	filter           textinput.Model
	rename           textinput.Model
	selection.Model
	// offset is the first visible row: the list scrolls under a
	// stationary viewport instead of clipping past it.
	drag     scrollDrag
	mode     sessionsMode
	flash    string
	reassign *sessionAssignment
	// root is the tasks dir scanned at open; mutations re-scan it so
	// the list always reflects disk, never a stale copy.
	root string
}

type sessionAssignment struct {
	directory string
	workspace string
	previous  string
}

// sessChrome counts border title, search, gap, hint and bottom border,
// plus mutation prompts. Each result has a title and an excerpt.
func (m *Model) sessChrome() int {
	chrome := 6
	if m.sessions.flash != "" {
		chrome++
	}

	if m.sessions.mode == sessRename {
		chrome += 2
	} else if m.sessions.mode == sessReassign {
		chrome += 2
	} else if m.sessions.mode == sessConfirm {
		chrome++
	}

	return chrome
}

// sessVisible is how many entries fit the viewport. Stateless math
// shared by keys and render, so paging and clipping never disagree.
func (m *Model) sessVisible() int {
	availH := m.termH - 3
	if m.ready {
		availH = m.transcript.Viewport.Height()
	}

	visible := min(6, (availH-m.sessChrome())/4)
	if visible > 0 {
		return visible
	}

	return 1
}

// clampOffset keeps the cursor on screen: follow down, pull up,
// pin to bounds after delete re-scans.
func (m *Model) clampOffset() {
	m.sessions.clamp(m.sessVisible())
}

// openSessions builds the list and pushes the overlay; closeSessions
// tears it down with focus restored. Balanced by construction: every
// open pairs with esc/enter or run start.
func (m *Model) openSessions() {
	filter := newSearchInput(max(popup.ModalWidth(m.termW-2)-6, 1))
	filter.Focus()

	m.sessions = &sessionsState{
		currentWorkspace: m.wsRoot,
		entries:          m.savedSessions,
		all:              m.savedSessions,
		loading:          m.sessionsLoading,
		root:             m.sessionRoot(),
		filter:           filter,
	}

	m.refreshSessions()
	m.pushOverlay()

	// Search overlays the existing panes without changing composer geometry.
	m.fitBottom()
	m.refreshContent()
}

func (m *Model) closeSessions() {
	m.sessions = nil
	m.popOverlay()
	m.fitBottom()
	m.refreshContent()
	if m.transcript.Follow {
		m.transcript.Viewport.GotoBottom()
	}
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
		if s.currentOnly && (e.workspace == "" || !sameWorkspace(e.workspace, s.currentWorkspace)) {
			continue
		}

		e.historyMatch, e.matchText = false, ""
		message, matched := e.findText(query)
		titleMatch := query == "" || strings.Contains(strings.ToLower(e.title+" "+e.id), query)

		if titleMatch {
			if query != "" && matched {
				e.matchText = message.text
			}

			titles = append(titles, e)
			continue
		}

		if matched {
			e.matchText = message.text
			e.matchMessage = message.message
			e.historyMatch = true
			history = append(history, e)
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

func (e sessionEntry) findText(query string) (sessionText, bool) {
	if query == "" {
		return sessionText{}, false
	}

	for _, message := range e.search {
		if strings.Contains(message.lower, query) {
			return message, true
		}
	}

	return sessionText{}, false
}

func (s *sessionsState) scrollTo(offset, visible int) {
	s.Offset = min(max(offset, 0), len(s.entries)-visible)
	s.Cursor = min(max(s.Cursor, s.Offset), s.Offset+visible-1)
}
