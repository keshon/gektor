package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/conversation"
)

// openSelected switches to the selected session: snapshot loads,
// transcript rebuilds from history, follow-ups continue the
// conversation via the normal Resume path. Only at rest: switching
// mid-run would orphan the in-flight turn.
func (m *Model) openSelected() tea.Cmd {
	s := m.sessions
	if len(s.entries) == 0 {
		return nil
	}

	if m.state != stDone {
		s.flash = "stop the run first"
		return nil
	}

	cur := s.entries[s.Cursor]
	stateFile := filepath.Join(cur.dir, "state.json")
	snapshot, err := conversation.Load(stateFile)
	if err != nil {
		s.flash = "Cannot read saved history: " + err.Error()
		return nil
	}

	root, err := snapshot.ResolveWorkspace()
	if err != nil {
		s.flash = "Workspace missing; CTRL+W"
		if snapshot.Workspace == "" {
			s.flash = "Workspace unassigned; CTRL+W"
		}

		return nil
	}

	if !sameWorkspace(root, m.wsRoot) {
		if m.application == nil {
			s.flash = "Reopen with -resume " + stateFile
			return nil
		}

		p := m.application.Preferences
		p.Workspace = root
		m.modeChange = &ModeChange{
			Preferences: p,
			History:     snapshot.History,
			Drafts:      m.relaunchDrafts(),
			Directory:   cur.dir,
			KeepAct:     !m.plan,
		}

		return tea.Quit
	}

	cmd, err := m.activateSavedSession(cur, snapshot)
	if err != nil {
		s.flash = err.Error()
	}

	return cmd
}

// activateSavedSession replaces Agent state only after the new harness session
// is constructed. A failed construction retains the current session and draft.
func (m *Model) activateSavedSession(cur sessionEntry, snapshot conversation.Snapshot) (tea.Cmd, error) {
	stateFile := filepath.Join(cur.dir, "state.json")
	history := snapshot.History
	task := conversation.TitleFor(history)
	if task == "" {
		task = cur.title
	}

	if m.newSession == nil {
		return nil, fmt.Errorf("sessions unavailable here")
	}

	sess, err := m.newSession(task, stateFile, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot open session: %w", err)
	}

	m.saveDraft()
	m.cancelSessionTitle()
	m.approvals.Reset()
	m.sess = sess
	m.stateFile = stateFile
	m.sessionTitle = cur.title
	m.resetRunFacts()
	m.restoreDraft()

	blocks, boundary := renderHistoryAt(history)
	m.transcript.blocks = append([]block{markerBlock("- opened " + cur.title + " -")}, blocks...)
	m.appendSavedRun(snapshot)
	m.restoreContext(history)
	m.retryRun = nil
	m.runErr = nil
	m.answer = ""
	m.steps = 0
	m.state = stDone

	m.closeSessions()
	m.fitBottom()
	m.refreshContent()

	// Land at the top: a stale offset into replaced blocks shows
	// mid-history or blank. follow stays on so live turns behave.
	m.transcript.Follow = true
	m.transcript.Viewport.GotoTop()
	if len(snapshot.Runs) > 0 && snapshot.Runs[len(snapshot.Runs)-1].Outcome != "completed" {
		m.transcript.Viewport.GotoBottom()
	}

	if cur.historyMatch {
		// Match indices refer to renderHistory; the opened marker adds one block.
		target := historyMatchBlock(blocks, boundary, cur.matchMessage) + 1
		row := m.transcript.PartOffset(target)
		m.transcript.Follow = false
		m.transcript.Viewport.SetYOffset(row)
	}

	m.restoreDraft()

	return m.input.Focus(), nil
}

// activeSessionTitle names the open session for the header and the
// sessions bottom bar: stored title, then dir id; a dash while a session is open
// without one yet. No history reads: the header renders every second.
func (m *Model) activeSessionTitle() string {
	if m.stateFile == "" {
		return "New session"
	}

	dir := filepath.Dir(m.stateFile)

	for _, e := range m.savedSessions {
		if sameSession(e.dir, m.stateFile) {
			return e.title
		}
	}

	if m.sessionTitle != "" {
		return m.sessionTitle
	}

	return filepath.Base(dir)
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
