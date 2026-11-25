package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/keshon/gektor/pkg/tui/screen"
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

const sessionPinFile = "pinned"

func sessionPinned(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, sessionPinFile))
	return err == nil && info.Mode().IsRegular()
}

func (m *Model) toggleActiveFavorite() {
	if m.stateFile == "" {
		return
	}
	dir := filepath.Dir(m.stateFile)
	target, err := browserSessionDirectory(m.sessionRoot(), dir)
	if err == nil {
		err = toggleSessionFavorite(target)
	}
	if err != nil {
		m.chrome.Open("Session favorite", []string{err.Error()})
		return
	}
	m.refreshSessions()
}

func toggleSessionFavorite(dir string) error {
	marker := filepath.Join(dir, sessionPinFile)
	if sessionPinned(dir) {
		return os.Remove(marker)
	}
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return file.Close()
}

func (s *sessionsState) toggleMark(dir string) {
	if s.marked[dir] {
		delete(s.marked, dir)
		return
	}
	if s.marked == nil {
		s.marked = map[string]bool{}
	}
	s.marked[dir] = true
}

func (s *sessionsState) markVisible() {
	if s.marked == nil {
		s.marked = make(map[string]bool, len(s.entries))
	}
	for _, entry := range s.entries {
		s.marked[entry.dir] = true
	}
}

func (m *Model) confirmSessionDeletion() {
	s := m.sessions
	var targets []sessionEntry
	for _, entry := range s.all {
		if s.marked[entry.dir] {
			targets = append(targets, entry)
		}
	}
	if len(targets) == 0 && len(s.entries) > 0 {
		targets = []sessionEntry{s.entries[s.Cursor]}
	}
	if len(targets) == 0 {
		return
	}

	title := fmt.Sprintf("Delete %d sessions?", len(targets))
	hint := "This permanently deletes the selected saved sessions."
	if len(targets) == 1 {
		title = "Delete session?"
		hint = fmt.Sprintf("Permanently delete %q?", cleanText(targets[0].title))
	}
	m.openForm(screen.Form{
		Title:         title,
		Hint:          hint,
		ApplyLabel:    "Delete",
		Destructive:   true,
		StartOnCancel: true,
	}, func(_ map[string]string) error {
		if m.sessions == s {
			m.deleteSelected(targets)
		}
		return nil
	})
	s.confirmation = m.chrome.DialogID()
}

func browserSessionDirectory(root, dir string) (string, error) {
	target, valid := sessionDeleteTarget(root, dir)
	if !valid {
		return "", fmt.Errorf("session is outside the session directory")
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("session path is not a directory")
	}
	return target, nil
}

func (m *Model) deleteSelected(targets []sessionEntry) {
	s := m.sessions
	s.flash = ""

	paths := make([]string, 0, len(targets))
	for _, entry := range targets {
		if sameSession(entry.dir, m.stateFile) && m.state != stDone {
			s.flash = "Stop the run before deleting its session."
			return
		}
		path, err := browserSessionDirectory(s.root, entry.dir)
		if err != nil {
			s.flash = "Cannot delete " + entry.id + ": " + err.Error()
			return
		}
		paths = append(paths, path)
	}

	deleted := 0
	for i, entry := range targets {
		if sameSession(entry.dir, m.stateFile) {
			m.newTask()
		}
		err := os.RemoveAll(paths[i])
		if err != nil {
			s.flash = fmt.Sprintf("Deleted %d of %d. Cannot delete %s: %v", deleted, len(targets), entry.id, err)
			break
		}
		deleted++
		delete(s.marked, entry.dir)
		delete(m.drafts, filepath.Join(entry.dir, "state.json"))
		m.savedSessions = withoutSession(m.savedSessions, entry.dir)
		s.all = withoutSession(s.all, entry.dir)
		s.entries = withoutSession(s.entries, entry.dir)
	}
	if deleted == len(targets) && deleted > 1 {
		s.flash = fmt.Sprintf("Deleted %d sessions", deleted)
	}
	m.clampOffset()
	m.refreshSessions()
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
