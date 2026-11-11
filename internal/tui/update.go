package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/tui/workflow"
)

// Update routes screen results and application input on one update loop.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch value := msg.(type) {
	case settings.DiscoveryRequest:
		cmd = m.discoverSettings(value)
	case settings.DetailMsg, settings.ScanMsg:
		m.acceptSettings(msg)
	case workflow.Message:
		if m.workflows != nil {
			_, cmd = m.workflows.Update(value)
		}
	case tea.WindowSizeMsg:
		m.termW, m.termH = value.Width, value.Height
		m.chrome.Props.Width, m.chrome.Props.Height = value.Width, value.Height
		_, cmd = m.agent.Update(value)
	case tea.KeyPressMsg:
		_, cmd = m.handleKey(value)
	case tea.MouseMsg:
		_, cmd = m.handleMouse(value)
	case tea.PasteMsg, tea.PasteStartMsg, tea.PasteEndMsg:
		if m.chrome.Dialog != nil {
			cmd = m.chrome.UpdateFormInput(msg)
		} else if !m.workflowActive() {
			_, cmd = m.agent.Update(msg)
		}
	default:
		_, cmd = m.agent.Update(msg)
	}
	var requests []tea.Cmd
	for _, request := range m.agent.TakeRequests() {
		switch request := request.(type) {
		case screen.Navigate:
			if request.Screen == "workflow" {
				requests = append(requests, m.workflowCommand(request.Action))
			} else if request.Screen == "agent" {
				m.showAgent()
			}
		case screen.ApplicationCommand:
			requests = append(requests, m.runAction(request.Command))
		case screen.QuitRequest:
			_, next := m.requestQuit()
			requests = append(requests, next)
		}
	}
	if m.pendingQuit {
		m.pendingQuit = false
		_, next := m.requestQuit()
		requests = append(requests, next)
	}
	if m.workflowActive() && !m.workflows.State().Active {
		m.showAgent()
	}
	m.syncPresentation()
	if _, resized := msg.(tea.WindowSizeMsg); resized {
		m.chrome.ScrollDialog(0)
	}
	return m, tea.Batch(cmd, tea.Batch(requests...), m.takeCommands())
}
func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quit = true
		m.Close()
		return m, tea.Quit
	case "ctrl+q":
		return m.requestQuit()
	}
	if m.agent.Status().Gated {
		_, cmd := m.agent.Update(msg)
		return m, cmd
	}
	if m.chrome.Dialog != nil {
		if m.chrome.Dialog.Table != nil {
			return m, m.chrome.TableInput(msg)
		}
		if m.chrome.Dialog.Form != nil {
			return m, m.chrome.FormKey(msg)
		}
		return m, m.chrome.InfoKey(msg)
	}
	if m.chrome.Header.Focused {
		return m, m.runAction(m.chrome.HeaderKey(msg))
	}
	switch msg.String() {
	case "f9":
		if !m.agent.Status().BrowserOpen {
			m.chrome.Header = screen.MenuState{Focused: true, Global: true}
		}
		return m, nil
	case "f10":
		return m.requestQuit()
	}
	if m.workflowActive() {
		_, cmd := m.workflows.Update(msg)
		return m, cmd
	}
	_, cmd := m.agent.Update(msg)
	return m, cmd
}
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.chrome.Dialog != nil && !m.agent.Status().Gated {
		mouse := msg.Mouse()
		if m.chrome.ActionBarVisible() && mouse.Y == m.termH-1 && mouse.Button == tea.MouseLeft && screen.IsMouseClick(msg) {
			if key, ok := m.chrome.ToolbarKey(mouse.X); ok {
				return m.handleKey(key)
			}
		}
		return m, m.chrome.DialogMouse(msg)
	}
	if !m.workflowActive() {
		_, cmd := m.agent.Update(msg)
		return m, cmd
	}
	if m.chrome.Dialog == nil && m.workflows.State().MouseCaptured {
		_, cmd := m.workflows.Update(msg)
		return m, cmd
	}
	mouse := msg.Mouse()
	if m.chrome.ActionBarVisible() && mouse.Y == m.termH-1 && mouse.Button == tea.MouseLeft && screen.IsMouseClick(msg) {
		if key, ok := m.chrome.ToolbarKey(mouse.X); ok {
			return m.handleKey(key)
		}
	}
	if handled, command := m.chrome.HeaderMouse(msg); handled {
		return m, m.runAction(command)
	}
	_, cmd := m.workflows.Update(msg)
	return m, cmd
}
func (m *Model) runAction(command string) tea.Cmd {
	if m.applicationAction(command) {
		return nil
	}
	switch command {
	case "":
		return nil
	case "quit":
		_, cmd := m.requestQuit()
		return cmd
	case "interrupt":
		m.quit = true
		m.Close()
		return tea.Quit
	case "screen:agent":
		m.showAgent()
		return nil
	case "screen:workflows":
		m.openWorkflows()
		return nil
	case "help-keys":
		if m.workflowActive() {
			return m.workflowCommand("help")
		}
	}
	if command == "/workflows" {
		m.openWorkflows()
		return nil
	}
	if strings.HasPrefix(command, "/workflows ") {
		return m.workflowCommand(strings.TrimPrefix(command, "/workflows "))
	}
	if command == "/sessions" || strings.HasPrefix(command, "/new") {
		m.showAgent()
	}
	return m.agent.Action(command)
}
