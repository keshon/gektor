package tui

import (
	"github.com/keshon/gektor/internal/tui/components/composer"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"os"
	"path/filepath"
	"time"
)

// Drafts are local UI state, separate from submitted model history.
type draft struct {
	text, queued         string
	images, queuedImages []string
	recall               composer.Snapshot
}

// Drafts carries unsent UI state across a workspace or backend relaunch.
// It is never submitted as conversation history or written into a session.
type Drafts map[string]draft

func (m *model) relaunchDrafts() Drafts {
	m.saveDraft()
	return m.drafts
}

func (m *model) saveDraft() {
	if m.drafts == nil {
		m.drafts = make(map[string]draft)
	}
	m.drafts[m.stateFile] = draft{text: m.input.Value(), queued: m.queued, recall: m.input.Snapshot(), images: append([]string(nil), m.draftImages...), queuedImages: append([]string(nil), m.queuedImages...)}
}
func (m *model) restoreDraft() {
	m.inspector = toolInspector{}
	m.fitColumns()
	m.attachmentEpoch++
	d := m.drafts[m.stateFile]
	m.input.SetValue(d.text)
	m.queuedVersion++
	m.queued = d.queued
	m.draftImages = append([]string(nil), d.images...)
	m.queuedImages = append([]string(nil), d.queuedImages...)
	m.input.Restore(d.recall)
	m.fitInput()
	m.fitBottom()
}

func (m *model) resetRunFacts() {
	m.cancelEvaluation()
	m.retryRun, m.runErr = nil, nil
	m.answer, m.live, m.gateDraft = "", "", ""
	m.resetLive()
	m.steps, m.toolsUsed = 0, 0
	m.progress.Phase = ""
	m.clearContext()
	m.tokensEst, m.interrupted = false, false
	m.filesTouched = nil
	m.elapsed = 0
	m.firstToken = time.Time{}
}

func (m *model) modeName() string {
	if m.plan {
		return "Plan"
	}
	return "Act"
}

func (m *model) saveMode() error {
	if m.stateFile != "" {
		if err := writeSavedMode(m.stateFile, m.plan); err != nil {
			return err
		}
	}
	return m.savePreferences()
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
	switch m.state {
	case stPermission:
		return m.controlHints(popup.Shortcut{Key: "Ctrl+Q", Action: "quit"}, popup.Shortcut{Key: "PgUp/PgDn", Action: "preview"})
	case stAsk:
		return m.controlHints(popup.Shortcut{Key: "Esc", Action: "stop"}, popup.Shortcut{Key: "Enter", Action: "answer"}, popup.Shortcut{Key: "Ctrl+O", Action: "newline"})
	case stRunning:
		return m.controlHints(popup.Shortcut{Key: "Esc", Action: "stop"}, popup.Shortcut{Key: "Enter", Action: "send"}, popup.Shortcut{Key: "Alt+Q", Action: "Queue"}, popup.Shortcut{Key: "Ctrl+O", Action: "newline"})
	case stStopping:
		return m.controlHints(popup.Shortcut{Key: "Ctrl+C", Action: "exit"})
	default:
		return m.controlHints(popup.Shortcut{Key: "Enter", Action: "send"}, popup.Shortcut{Key: "Ctrl+O", Action: "newline"})
	}
}
