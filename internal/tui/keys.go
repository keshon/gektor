package tui

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"github.com/keshon/gektor/internal/agent"
	"strings"
)

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	defer m.syncFocus()
	if m.dialog != nil && m.dialog.table != nil && msg.String() != "ctrl+c" && msg.String() != "ctrl+q" {
		return m, m.tableInput(msg)
	}
	if m.dialog != nil && m.dialog.form != nil && msg.String() != "ctrl+c" && msg.String() != "ctrl+q" {
		return m.formKey(msg)
	}
	if msg.String() == "ctrl+q" {
		return m.requestQuit()
	}
	if m.workflowActive() && m.dialog == nil && !m.header.focused && m.sessions == nil && m.header.menu == "" && msg.String() != "ctrl+c" {
		return m.workflowKey(msg)
	}
	if m.state == stPermission && m.dialog == nil && msg.String() == "f4" && m.gatePreview != "" {
		return m.gateKey(msg)
	}
	switch msg.String() {
	case "ctrl+p":
		return m.footerKey(tea.KeyPressMsg{Code: tea.KeyF2})
	case "ctrl+n":
		return m.footerKey(tea.KeyPressMsg{Code: tea.KeyF4})
	}
	if handled, cmd := m.inspectorKey(msg); handled {
		return m, cmd
	}
	if m.activeInputOwner() == focusHeader {
		return m.headerKey(msg)
	}
	switch msg.String() {
	case "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10":
		return m.footerKey(msg)
	}
	if msg.String() == "ctrl+q" || msg.String() == "ctrl+c" {
		m.quit = true
		m.cancel()
		return m, tea.Quit
	}
	if m.dialog == nil && (m.state == stPermission || m.state == stAsk) {
		if msg.String() == "pgup" || msg.String() == "pgdown" || msg.String() == "ctrl+end" {
			var cmd tea.Cmd
			if msg.String() == "ctrl+end" {
				m.gateVP.GotoBottom()
				return m, nil
			}
			m.gateVP, cmd = m.gateVP.Update(msg)
			return m, cmd
		}
	}
	if m.activeInputOwner() == focusInput {
		if m.state != stAsk && msg.Mod == tea.ModAlt && msg.Code >= '1' && msg.Code <= '8' {
			m.detachImage(string(rune(msg.Code)))
			return m, nil
		}
		switch msg.String() {
		case "alt+q":
			if m.state != stAsk {
				m.openInputQueue()
			}
			return m, nil
		case "ctrl+v", "ctrl+alt+v":
			if m.state != stAsk {
				return m, m.pasteClipboard(msg.String() == "ctrl+alt+v")
			}
		case "ctrl+end":
			m.transcript.Viewport.GotoBottom()
			m.transcript.Follow = true
			return m, nil
		case "ctrl+u":
			m.input.SetValue("")
			if m.state != stAsk {
				m.attachmentEpoch++
				m.draftImages = nil
			}
			m.fitInput()
			m.fitBottom()
			return m, nil
		case "ctrl+x":
			if m.state != stAsk && m.sess != nil {
				if _, ok := m.sess.RecallInput(); ok {
					m.fitBottom()
					return m, nil
				}
			}
			if m.hasQueued() {
				m.queuedVersion++
				m.queued = ""
				m.queuedImages = nil
				m.fitBottom()
				return m, nil
			}
		case "ctrl+e":
			if m.state != stAsk && m.pendingInputCount() > 0 {
				if m.input.Value() != "" || len(m.draftImages) > 0 || m.clipboardBusy {
					m.appendBlock(markerBlock("Keep or clear the current draft before editing the queue."))
					return m, nil
				}
				if msg, ok := m.sess.RecallInput(); ok {
					m.attachmentEpoch++
					m.input.SetValue(msg.Content)
					m.draftImages = msg.Images
					m.fitInput()
					m.fitBottom()
					return m, m.input.Focus()
				}
				m.appendBlock(markerBlock("The agent already received that message."))
				return m, nil
			}
			if m.hasQueued() && m.state != stAsk {
				if m.input.Value() != "" || len(m.draftImages) > 0 {
					m.appendBlock(markerBlock("Keep or clear the current draft before editing the queue."))
					return m, nil
				}
				m.input.SetValue(m.queued)
				m.draftImages, m.queuedImages = m.queuedImages, nil
				m.queuedVersion++
				m.queued = ""
				m.fitInput()
				m.fitBottom()
				return m, m.input.Focus()
			}
		}
	}
	m.syncSuggestions()
	if cmd, ok := m.suggestionKey(msg); ok {
		// The popup owns this key: Enter either runs a bare command or
		// inserts one that still needs an argument.
		return m, cmd
	}
	if m.activeInputOwner() == focusInput && (m.state == stDone || m.state == stAsk || m.state == stRunning) && msg.Mod.Contains(tea.ModAlt) && (msg.Code == tea.KeyUp || msg.Code == tea.KeyDown) {
		m.input.Recall(msg.Code == tea.KeyUp)
		m.fitInput()
		m.fitBottom()
		return m, nil
	}
	if m.sessions != nil && m.state != stPermission {
		// The sessions screen owns its keys like a dialog — except
		// over a permission gate, which is always on top: its y/n/a
		// answers must never land in a list.
		return m.sessionsKey(msg)
	}
	if m.dialog != nil {
		return m.infoDialogKey(msg)
	}

	if msg.String() == "ctrl+g" {
		return m.footerKey(tea.KeyPressMsg{Code: tea.KeyF3})
	}

	return m.conversationKey(msg)
}

func isMouseClick(msg tea.MouseMsg) bool { _, ok := msg.(tea.MouseClickMsg); return ok }

func (m *model) infoDialogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if (msg.String() == "s" || msg.String() == "S") && m.dialog.save != nil {
		if err := m.dialog.save(); err != nil {
			m.dialog.lines = append(m.dialog.lines, "", "Could not save: "+err.Error())
			m.dialog.offset = 0
		} else {
			m.closeDialog()
		}
		return m, nil
	}
	if (msg.String() == "r" || msg.String() == "R" || msg.String() == "e" || msg.String() == "E") && m.dialog.review != nil {
		m.dialog.review()
		return m, nil
	}
	// Takeover: the dialog eats every key but close and quit so
	// typing can neither reach the input nor toggle state behind it.
	// Close is esc/enter only: "q" must stay typable for dialogs
	// with inputs tomorrow. Confirmations are answered above, before
	// this branch; an info dialog has nothing to confirm.
	switch msg.String() {
	case "n", "N", "esc":
		m.closeDialog()
		return m, nil
	case "enter":
		m.closeDialog()
		return m, nil
	case "ctrl+c":
		m.quit = true
		m.cancel()
		return m, tea.Quit
	}
	if !m.dialog.help {
		switch msg.String() {
		case "down":
			m.scrollDialog(1)
		case "up":
			m.scrollDialog(-1)
		case "pgdown":
			m.scrollDialog(max(m.transcript.Viewport.Height()/2, 1))
		case "pgup":
			m.scrollDialog(-max(m.transcript.Viewport.Height()/2, 1))
		}
	}
	if m.dialog.help {
		switch msg.String() {
		case "tab", "right":
			m.dialog.page = (m.dialog.page + 1) % len(m.dialogHelpSections())
			m.dialog.offset = 0
		case "shift+tab", "left":
			m.dialog.page = (m.dialog.page + len(m.dialogHelpSections()) - 1) % len(m.dialogHelpSections())
			m.dialog.offset = 0
		case "down":
			m.scrollDialog(1)
		case "up":
			m.scrollDialog(-1)
		case "pgdown":
			m.scrollDialog(max(m.transcript.Viewport.Height()/2, 1))
		case "pgup":
			m.scrollDialog(-max(m.transcript.Viewport.Height()/2, 1))
		}
	}
	return m, nil
}

func (m *model) conversationKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stPermission:
		return m.gateKey(msg)
	case stAsk:
		switch msg.String() {
		case "esc":
			if answer := m.input.Value(); answer != "" {
				if m.gateDraft != "" {
					answer = m.gateDraft + "\n" + answer
				}
				m.input.SetValue(answer)
				m.gateDraft = ""
				m.fitInput()
			}
			m.interrupted = true
			m.cancel()
			m.state = stStopping
			m.fitBottom()
			return m, nil
		case "pgup", "pgdown":
			// Paging scrolls the transcript in every state; the
			// input keeps arrows and typing only.
			return m.scrollViewport(msg)
		case "enter":
			m.answerAsk(m.input.Value())
			return m, nil
		case "ctrl+c", "ctrl+q":
			m.quit = true
			m.cancel()
			return m, tea.Quit

		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.fitInput()
		m.fitBottom()
		return m, cmd
	case stDone:
		switch msg.String() {
		case "pgup", "pgdown":
			// Same routing as ask: paging belongs to the transcript.
			return m.scrollViewport(msg)
		case "enter":
			return m.followUp()
		case "esc":
			return m, nil
		case "ctrl+c", "ctrl+q":
			// Quit lives on ctrl+q (and ctrl+c): a letter key must
			// never quit, or words starting with q ("queen") become
			// untypable on an empty box. Empty+enter still quits.
			m.quit = true
			m.cancel()
			return m, tea.Quit

		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.fitInput()
		m.fitBottom()
		return m, cmd
	default:
		switch msg.String() {
		case "ctrl+c", "ctrl+q":
			m.quit = true
			m.cancel()
			return m, tea.Quit
		case "esc":
			if m.state != stStopping {
				m.interrupted = true
				m.cancel()
				m.state = stStopping
				m.fitBottom()
			}
			return m, nil
		case "enter":
			if m.state == stStopping {
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			if text == "" && len(m.draftImages) == 0 {
				return m, nil
			}
			// A command that acts on the view, or on the agent's own
			// services, runs now. Queuing it behind the run means the one
			// command you reach for mid-run - /stop, /latest, a theme
			// switch - waits for the run it acts on. Completeness is not
			// re-checked here: the picker already ran the commands that
			// were ready when it held Enter, so what reaches this branch
			// is text the user finished typing.
			if runsImmediate(text) {
				m.input.SetValue("")
				m.fitInput()
				m.fitBottom()
				return m.command(text)
			}
			if !strings.HasPrefix(text, "/") && m.sess != nil {
				resolved, images, err := m.draftAttachments(text)
				if err == nil {
					err = m.sess.Submit(resolved, images...)
				}
				if err == nil {
					m.input.Push(text)
					m.draftImages = nil
					m.input.SetValue("")
					m.fitInput()
					m.fitBottom()
					m.transcript.Follow = true
					m.transcript.Viewport.GotoBottom()
					return m, nil
				}
				if !errors.Is(err, agent.ErrInputClosed) {
					m.appendBlock(errorBlock("Cannot send message: " + err.Error()))
					return m, nil
				}
				// Completion raced this key: preserve the existing follow-up path.
			}
			if m.hasQueued() {
				m.appendBlock(markerBlock("a follow-up is already queued"))
				return m, nil
			}
			if m.clipboardBusy {
				m.appendBlock(errorBlock("wait for clipboard capture to finish"))
				return m, nil
			}
			m.queuedVersion++
			m.queued = text
			m.queuedImages, m.draftImages = m.draftImages, nil
			m.input.SetValue("")
			m.appendBlock(markerBlock("queued follow-up: " + truncate(m.queuedLabel(), 120)))
			m.fitInput()
			m.fitBottom()
			m.transcript.Follow = true
			m.transcript.Viewport.GotoBottom()
			return m, nil
		case "pgup", "pgdown":
			return m.scrollViewport(msg)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.fitInput()
		m.fitBottom()
		return m, cmd
	}
}
