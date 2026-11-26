package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/settings"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/screen"
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

	if m.preferences != nil && m.chrome.DialogID() == m.preferencesDialog {
		m.preparePreferences()
	}
	m.syncSettingsDiscovery()
	m.agent.PrepareControls()
	m.chrome.Prepare()
	if changed && m.active == agentScreen {
		m.agent.ControlsChanged()
	}

	m.syncPresentation()
	return commands
}

func (m *Model) settingsCommands() []tea.Cmd {
	request, ok := m.chrome.FormDetailRequest(m.chrome.DialogID())
	if !ok {
		return nil
	}
	command := request.Command()
	if m.preferences != nil && m.preferencesDialog == m.chrome.DialogID() && request.Field == "load-profile" {
		command = settings.ProfileDetail(request)
	}
	return []tea.Cmd{command}
}

func (m *Model) acceptSettings(msg tea.Msg) {
	switch msg := msg.(type) {
	case fieldform.DetailMsg:
		m.chrome.AcceptFormDetail(msg)
	case fieldform.ScanMsg:
		if job := m.settingsDiscovery; job != nil && job.picker == msg.ID {
			m.cancelSettingsDiscovery()
		}
		m.chrome.AcceptFormScan(msg)
	}
}

func (m *Model) applicationMenus(idle bool) []screen.Menu {
	editable := idle && m.profile != nil
	section := func(id string) screen.MenuItem {
		return screen.MenuItem{Text: settings.SectionTitle(id) + "…", Command: "edit:" + id, Enabled: editable}
	}
	menu := screen.Menu{ID: "settings", Label: "Settings", Items: []screen.MenuItem{
		{Text: "Appearance…", Command: "appearance", Enabled: true},
		{},
		section("workspace"), section("model"), section("generation"), section("profiles"),
		{},
		section("network"), section("you"), section("prompts"),
	}}
	help := screen.Menu{ID: "help", Label: "Help", Items: []screen.MenuItem{{Text: "Actions…", Command: "actions", Shortcut: "F12", Enabled: true}, {Text: "Keyboard shortcuts", Command: "help-keys", Enabled: true}}}
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

	if event.ID == m.providerKeyDialog && m.providerKeyDialog != (screen.DialogID{}) {
		switch event.Kind {
		case screen.SecretApply:
			m.applyProviderKey(event.ID)
		case screen.Closed:
			m.providerKeyDialog = screen.DialogID{}
			m.providerKeyKind = ""
			m.providerKeyURL = ""
			m.chrome.ResumeID(m.preferencesDialog)
		case screen.Replaced, screen.Suspended:
			m.preferences = nil
			m.preferencesDialog = screen.DialogID{}
			m.form = pendingForm{}
			m.providerKeyDialog = screen.DialogID{}
			m.providerKeyKind = ""
			m.providerKeyURL = ""
		}
	}

	if event.ID == m.toolsDialog && m.toolsDialog != (screen.DialogID{}) {
		if event.Kind == screen.SecretApply && m.chrome.DialogID() == event.ID && m.sshSecret != nil {
			m.applySSHSecret(event.ID)
		}
		if event.Kind == screen.TableAction && m.chrome.DialogID() == event.ID {
			commands = append(commands, m.toolAction(event))
		}
		if event.Kind == screen.Closed || event.Kind == screen.Replaced || event.Kind == screen.Suspended {
			page := m.toolsPage
			if page == toolSSHSecret {
				m.sshSecret = nil
			}
			m.tools.Close()
			m.toolsDialog = screen.DialogID{}
			if event.Kind == screen.Closed && m.modeChange == nil {
				if page == toolSSHEdit || page == toolSSHDetails || page == toolSSHSecret {
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
	case screen.TableAction:
		if m.actionsDialog == event.ID && m.chrome.DialogID() == event.ID && event.Action == "details" {
			command := event.Row
			m.chrome.Close()
			m.syncPresentation()
			for _, menu := range m.chrome.Props.Menus {
				for _, item := range menu.Items {
					if item.Command == command && item.Enabled {
						commands = append(commands, m.runAction(command))
						return commands
					}
				}
			}
		}
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
		if m.form.dialog == event.ID && m.chrome.DialogID() == event.ID {
			err := m.applySubmission(event.ID, event.Values)
			m.chrome.CompleteFormID(event.ID, err)
		}

	case screen.SaveCopy:
		if m.form.dialog == event.ID && m.chrome.DialogID() == event.ID {
			m.saveSettingsCopy()
		}
	}
	commands = append(commands, m.agent.HandleControl(event))
	if m.workflows != nil {
		commands = append(commands, m.workflows.HandleControl(event))
	}
	if event.Kind == screen.Closed || event.Kind == screen.Replaced || event.Kind == screen.Suspended {
		if m.actionsDialog == event.ID {
			m.actionsDialog = screen.DialogID{}
		}
		if event.Kind == screen.Suspended && m.preferencesDialog == event.ID && m.providerKeyDialog != (screen.DialogID{}) {
			return commands
		}
		if m.preferencesDialog == event.ID {
			m.preferences = nil
			m.preferencesDialog = screen.DialogID{}
		}
		if m.form.dialog == event.ID {
			m.form = pendingForm{}
		}
	}
	return commands
}

// pendingForm belongs to the feature, while shared controls own the editable draft.
type pendingForm struct {
	dialog screen.DialogID
	apply  func(map[string]string) error
}

func (m *Model) openForm(form screen.Form, apply func(map[string]string) error) {
	m.chrome.PresentForm(form)
	m.form = pendingForm{dialog: m.chrome.DialogID(), apply: apply}
}

func (m *Model) applySubmission(dialog screen.DialogID, values map[string]string) error {
	if dialog == (screen.DialogID{}) || m.form.apply == nil || m.form.dialog != dialog || m.chrome.DialogID() != dialog {
		return nil
	}
	return m.form.apply(values)
}
