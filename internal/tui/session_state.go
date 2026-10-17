package tui

import (
	"github.com/keshon/tars/internal/tui/components/composer"
	"os"
	"path/filepath"
	"time"
)

// Drafts are local UI state, separate from submitted model history.
type draft struct {
	text, queued string
	recall       composer.Snapshot
}

func (m *model) saveDraft() {
	if m.drafts == nil {
		m.drafts = make(map[string]draft)
	}
	m.drafts[m.stateFile] = draft{m.input.Value(), m.queued, m.input.Snapshot()}
}
func (m *model) restoreDraft() {
	d := m.drafts[m.stateFile]
	m.input.SetValue(d.text)
	m.queued = d.queued
	m.input.Restore(d.recall)
	m.fitInput()
	m.fitBottom()
}

func (m *model) resetRunFacts() {
	m.retryRun, m.runErr = nil, nil
	m.answer, m.live, m.gateDraft = "", "", ""
	m.resetLive()
	m.steps, m.toolsUsed = 0, 0
	m.clearContext()
	m.tokensEst, m.interrupted = false, false
	m.filesTouched = nil
	m.elapsed = 0
	m.firstToken = time.Time{}
}

func (m *model) modeName() string {
	if m.mission {
		return "Mission"
	}
	if m.plan {
		return "Plan"
	}
	return "Act"
}

func (m *model) saveMode() error {
	if m.stateFile == "" || m.mission {
		return nil
	}
	if err := writeSavedMode(m.stateFile, m.plan); err != nil {
		return err
	}
	return m.saveSessionSettings()
}

func writeSavedMode(stateFile string, plan bool) error {
	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		return err
	}
	mode := "act\n"
	if plan {
		mode = "plan\n"
	}
	return os.WriteFile(filepath.Join(filepath.Dir(stateFile), "mode"), []byte(mode), 0644)
}

func (m *model) actionHint() string {
	if m.navFocused {
		return "↑↓ choose  Enter open  Ctrl+R rename  Ctrl+D delete  Tab input"
	}
	switch m.state {
	case stPermission:
		return "PgUp/PgDn preview  Ctrl+Q quit"
	case stAsk:
		return "Enter answer  Shift+Enter newline  Esc stop"
	case stRunning:
		return "Enter queue  Shift+Enter newline  Esc stop"
	case stStopping:
		return "Stopping…  draft is preserved  Ctrl+C exit"
	default:
		hint := "Enter send  Shift+Enter newline"
		if m.sidebarVisible() {
			hint += "  Tab switch pane"
		}
		return hint
	}
}
