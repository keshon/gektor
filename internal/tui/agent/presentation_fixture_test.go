package agent

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/screen"
)

// Local screen tests supply controls just as the application parent does.
func (m *Model) syncControls() { m.settleTestControls(); m.chrome.Props = m.Presentation() }

func (m *Model) applyTheme(name string) { screen.SetTheme(name); m.RefreshTheme() }
func (m *Model) testView() tea.View     { m.syncControls(); return m.chrome.Compose(m.View()) }

func (m *Model) bindControls() { m.syncControls() }

func (m *Model) settleTestControls() {
	changed := false
	for {
		events := m.chrome.TakeEvents()
		if len(events) == 0 {
			break
		}
		for _, event := range events {
			switch event.Kind {
			case screen.Opened:
				changed = true
				m.OverlayOpened(event.First)
			case screen.Closed, screen.Suspended:
				changed = true
				m.OverlayClosed()
			}
			cmd := m.HandleControl(event)
			if cmd != nil {
				m.pendingCommands = append(m.pendingCommands, cmd)
			}
		}
	}
	m.PrepareControls()
	m.chrome.Prepare()
	if changed {
		m.ControlsChanged()
	}
}
func (m *Model) applyTestForm() { m.chrome.ApplyForm(); m.syncControls() }

func (m *Model) testUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.syncControls()
	model, cmd := m.Update(msg)
	m.syncControls()
	if _, resized := msg.(tea.WindowSizeMsg); resized {
		m.chrome.ScrollDialog(0)
	}
	return model, cmd
}
func (m *Model) testKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.syncControls()
	model, cmd := m.handleKey(msg)
	m.syncControls()
	return model, cmd
}

func (m *Model) testControls() *screen.Controls { m.syncControls(); return m.chrome }
