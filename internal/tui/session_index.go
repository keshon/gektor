package tui

import (
	tea "charm.land/bubbletea/v2"
)

type sessionsLoadedMsg struct {
	revision uint64
	root     string
	entries  []sessionEntry
	cache    map[string]cachedSession
}

// Commands capture immutable inputs. Only Update accepts their results into UI state.
func (m *model) refreshSessions() {
	m.sessionRevision++
	m.sessionsLoading = true
	if m.sessions != nil {
		m.sessions.loading = true
	}
	revision, root, cache, search := m.sessionRevision, m.sessionRoot(), m.sessionIndex, m.sessions != nil
	m.pendingCommands = append(m.pendingCommands, func() tea.Msg {
		entries, index := scanSessions(root, cache, search)
		return sessionsLoadedMsg{revision, root, entries, index}
	})
}
func (m *model) takeCommands() tea.Cmd {
	commands := m.pendingCommands
	m.pendingCommands = nil
	return tea.Batch(commands...)
}
func (m *model) acceptSessions(msg sessionsLoadedMsg) {
	if msg.revision != m.sessionRevision || msg.root != m.sessionRoot() {
		return
	}
	m.savedSessions, m.sessionIndex = msg.entries, msg.cache
	m.sessionsLoading = false
	if m.sessions != nil {
		m.sessions.replaceEntries(msg.entries)
		m.sessions.loading = false
	}
}

func scanSessions(root string, previous map[string]cachedSession, search bool) ([]sessionEntry, map[string]cachedSession) {
	summaries, cache := scanSavedSessions(root, previous, search)
	entries := make([]sessionEntry, 0, len(summaries))
	for _, summary := range summaries {
		e := sessionEntry{id: summary.ID, dir: summary.Dir, title: summary.Title, msgs: summary.Msgs, updated: summary.Updated, status: summary.Status, mode: summary.Mode, preview: summary.Preview}
		for _, text := range summary.Search {
			e.search = append(e.search, sessionText{text: text.Text, message: text.Message, lower: text.lower})
		}
		entries = append(entries, e)
	}
	return entries, cache
}
