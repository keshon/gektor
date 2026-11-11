package agent

import (
	"os"
	"path/filepath"
)

// reassignSelected is reached only after an explicit confirmation.
func (m *Model) reassignSelected() {
	s := m.sessions
	s.mode = sessList
	assignment := s.reassign
	s.reassign = nil
	if m.state != stDone || assignment == nil {
		return
	}

	if !sameWorkspace(assignment.workspace, m.wsRoot) {
		s.flash = "Workspace changed. Assign again."
		return
	}

	found := false

	for _, entry := range s.entries {
		if entry.dir == assignment.directory {
			found = true
			break
		}
	}

	if !found {
		s.flash = "Session changed. Select it again."
		return
	}

	assigned, err := reassignSessionWorkspace(*assignment)
	if err != nil {
		s.flash = "Cannot assign. ESC for details."
		m.appendBlock(errorBlock("Cannot assign workspace: " + err.Error()))
		return
	}

	if !assigned {
		s.flash = "Session workspace changed. Assign again."
		return
	}

	s.flash = "Workspace assigned."
	m.refreshSessions()
}

// commitRename stores the title beside the session (empty restores the
// derived text) and refreshes the row in place: order never moves
// under the cursor, only the title changes.
func (m *Model) commitRename() {
	s := m.sessions
	if len(s.entries) == 0 {
		s.mode = sessList
		return
	}

	title := s.rename.Value()
	s.rename.Blur()
	s.rename.SetValue("")
	s.mode = sessList

	cur := &s.entries[s.Cursor]
	name, err := renameSavedSession(cur.dir, cur.id, title)
	if err != nil {
		s.flash = "rename failed: " + err.Error()
		return
	}

	s.flash = ""
	cur.title = name
	m.updateSessionTitle(cur.dir, name)
	m.refreshSessions()
	if s.all != nil {
		s.replaceEntries(s.all)
	}
}

// deleteSelected removes the session dir — title goes with it, so no
// orphan rows — then re-scans. Deleting the active session unloads it
// first (the empty state, then the dir is just files). Only at rest:
// unloading mid-run would orphan the in-flight turn.
func (m *Model) deleteSelected() {
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

	target, valid := sessionDeleteTarget(s.root, cur.dir)
	if !valid {
		s.mode, s.flash = sessList, "refusing to delete outside the session directory"
		return
	}

	err := os.RemoveAll(target)
	if err != nil {
		s.mode = sessList
		s.flash = "delete failed: " + err.Error()
		return
	}

	delete(m.drafts, filepath.Join(cur.dir, "state.json"))
	m.savedSessions = withoutSession(m.savedSessions, cur.dir)
	s.all = withoutSession(s.all, cur.dir)
	s.entries = withoutSession(s.entries, cur.dir)
	m.refreshSessions()

	if s.Cursor >= len(s.entries) && s.Cursor > 0 {
		s.Cursor--
	}

	m.clampOffset()
	s.mode = sessList
	s.flash = ""
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

func (m *Model) updateSessionTitle(dir, title string) {
	if m.titleJob != nil && filepath.Dir(m.titleJob.stateFile) == dir {
		m.cancelSessionTitle()
	}

	for i := range m.savedSessions {
		if m.savedSessions[i].dir == dir {
			m.savedSessions[i].title = title
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
