package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
)

func (m *Model) settleControls() []tea.Cmd {
	var commands []tea.Cmd
	changed := false
	for {
		events := m.chrome.TakeEvents()
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			commands = append(commands, m.dispatchControl(event)...)
			if event.Kind == screen.Opened || event.Kind == screen.Closed || event.Kind == screen.Suspended {
				changed = true
			}
		}
	}

	m.agent.PrepareControls()
	m.chrome.Prepare()
	if changed && m.active == agentScreen {
		m.agent.ControlsChanged()
	}

	m.syncPresentation()
	return commands
}

func (m *Model) settingsCommands() []tea.Cmd {
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil {
		return nil
	}
	if cmd := m.chrome.Dialog.Form.DetailCommand(); cmd != nil {
		return []tea.Cmd{cmd}
	}
	return nil
}

func (m *Model) acceptSettings(msg tea.Msg) {
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil {
		return
	}
	switch msg := msg.(type) {
	case settings.DetailMsg:
		m.chrome.Dialog.Form.AcceptDetail(msg)
	case settings.ScanMsg:
		m.chrome.Dialog.Form.AcceptScan(msg)
	}
}

func (m *Model) applicationMenus(idle bool) []screen.Menu {
	editable := idle && m.profile != nil
	section := func(id string) screen.MenuItem {
		return screen.MenuItem{Text: settings.SectionTitle(id) + "…", Command: "edit:" + id, Enabled: editable}
	}
	menu := screen.Menu{ID: "settings", Label: "Settings", Items: []screen.MenuItem{
		{Text: "Appearance…", Command: "appearance", Enabled: true},
		{Text: "Tools…", Command: "tools", Enabled: idle},
		{},
		section("model"), section("generation"), section("profiles"),
		{},
		section("you"), section("prompts"),
	}}
	help := screen.Menu{ID: "help", Label: "Help", Items: []screen.MenuItem{{Text: "Keyboard shortcuts", Command: "help-keys", Enabled: true}}}
	if !m.workflowActive() {
		help.Items = append(help.Items, screen.MenuItem{Text: "Commands", Command: "help-commands", Enabled: true})
		if m.profile != nil {
			help.Items = append(help.Items, screen.MenuItem{Text: "Inspect agent instructions…", Command: "prompt-info", Enabled: true})
		}
	}
	help.Items = append(help.Items, screen.MenuItem{}, screen.MenuItem{Text: "About GEKTOR", Command: "about", Enabled: true})
	return []screen.Menu{menu, help}
}

func (m *Model) dispatchControl(event screen.Event) []tea.Cmd {
	var commands []tea.Cmd

	if event.Dialog == m.toolsDialog {
		if event.Kind == screen.TableAction {
			commands = append(commands, m.toolAction(event))
		}
		if event.Kind == screen.Closed || event.Kind == screen.Replaced || event.Kind == screen.Suspended {
			page := m.toolsPage
			m.tools.Close()
			m.toolsDialog = nil
			if event.Kind == screen.Closed && m.modeChange == nil {
				if page == toolSSHEdit || page == toolSSHDetails {
					m.openSSHTools()
				} else if page == toolMediaEdit || page == toolMediaDetails {
					m.openMediaTools()
				} else if page == toolMedia || page == toolSSH || page == toolDetails {
					m.openTools()
				}
			}
		}
	}
	switch event.Kind {
	case screen.Opened:
		if event.First {
			m.overlayScreen = m.active
		}
		if m.overlayScreen == agentScreen {
			m.agent.OverlayOpened(event.First)
		}

	case screen.Closed, screen.Suspended:
		if m.overlayScreen == agentScreen {
			m.agent.OverlayClosed()
		}

	case screen.Apply:
		if m.form.dialog == event.Dialog && m.chrome.Dialog == event.Dialog {
			err := m.applySubmission(event.Dialog, event.Values)
			m.chrome.CompleteForm(event.Dialog, err)
		}

	case screen.SaveCopy:
		if m.form.dialog == event.Dialog && m.chrome.Dialog == event.Dialog {
			m.saveSettingsCopy()
		}
	}
	commands = append(commands, m.agent.HandleControl(event))
	if m.workflows != nil {
		commands = append(commands, m.workflows.HandleControl(event))
	}
	if event.Kind == screen.Closed || event.Kind == screen.Replaced || event.Kind == screen.Suspended {
		if m.form.dialog == event.Dialog {
			m.form = pendingForm{}
		}
	}
	return commands
}

// pendingForm belongs to the feature, while shared controls own the editable draft.
type pendingForm struct {
	dialog *screen.DialogState
	apply  func(map[string]string) error
}

func (m *Model) openForm(form screen.Form, apply func(map[string]string) error) *settings.Model {
	draft := m.chrome.PresentForm(form)
	m.form = pendingForm{dialog: m.chrome.Dialog, apply: apply}
	return draft
}

func (m *Model) applySubmission(dialog *screen.DialogState, values map[string]string) error {
	if m.form.dialog != dialog || m.chrome.Dialog != dialog {
		return nil
	}
	return m.form.apply(values)
}
