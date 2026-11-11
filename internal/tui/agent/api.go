package agent

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

// Status exposes only facts needed by the application shell's routing policy.
type Status struct{ Idle, Gated, BrowserOpen bool }

// Status returns routing flags without exposing the conversation or run.
func (m *Model) Status() Status {
	return Status{Idle: m.state == stDone, Gated: m.state == stAsk || m.state == stPermission, BrowserOpen: m.sessions != nil}
}

// Action handles an Agent command chosen by application controls.
func (m *Model) Action(command string) tea.Cmd {
	if strings.HasPrefix(command, "/") {
		_, cmd := m.command(command)
		return cmd
	}
	_, cmd := m.activateMenuAction(command)
	return cmd
}

// Hide releases transient conversation UI while retaining history and the draft.
func (m *Model) Hide() {
	m.hidden = true
	m.attachmentEpoch++
	m.cancelEvaluation()
	m.cancelSessionTitle()
	m.inspector = toolInspector{}
	if m.sessions != nil {
		m.closeSessions()
	}
	m.fitBottom()
}

// Activate returns input ownership to the retained Agent screen.
func (m *Model) Activate() { m.hidden = false; m.syncFocus() }

// Refresh reconciles focus and control effects after an application interaction.
func (m *Model) Refresh() tea.Cmd {
	m.syncSuggestions()
	m.syncFocus()
	m.syncComposerHeight()
	m.chrome.ResizeFormEditor()
	return m.takeCommands()
}

func (m *Model) request(msg tea.Msg) tea.Cmd { m.requests = append(m.requests, msg); return nil }

// TakeRequests transfers navigation and quit intents to the application once.
func (m *Model) TakeRequests() []tea.Msg { requests := m.requests; m.requests = nil; return requests }

// Snapshot captures the retained conversation and unsent input for a settings
// relaunch. History is read before the caller writes a replacement profile.
// On a history read failure, the returned snapshot still contains unsent input.
func (m *Model) Snapshot() (*ModeChange, error) {
	snapshot := &ModeChange{
		Draft:     m.input.Value(),
		Images:    append([]string(nil), m.draftImages...),
		Drafts:    m.relaunchDrafts(),
		Directory: m.sessionDirectory(),
		KeepAct:   !m.plan,
	}
	history := append([]llm.Message(nil), m.seedHistory...)
	if m.stateFile != "" {
		var err error
		history, err = conversation.ReadHistory(m.stateFile)
		if err != nil {
			return snapshot, err
		}
	}
	snapshot.History = history
	return snapshot, nil
}

// Appearance returns the current conversation display choices.
func (m *Model) Appearance() config.Appearance { return m.currentAppearance() }

// ApplyAppearance updates local layout and caches after application settings change.
func (m *Model) ApplyAppearance(appearance config.Appearance) { m.applyAppearance(appearance) }

// OverlayOpened releases local input while application controls own focus.
func (m *Model) OverlayOpened(first bool) {
	m.cancelEvaluation()
	if first {
		m.pushOverlay()
	}
	m.input.Blur()
}

// OverlayClosed restores the conversation focus saved on opening an overlay.
func (m *Model) OverlayClosed() { m.cancelEvaluation(); m.popOverlay() }

// ControlsChanged reconciles the conversation with shared control geometry.
func (m *Model) ControlsChanged() { m.fitBottom(); m.syncFocus() }
