package agent

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/screen"
)

// Local screen tests supply controls just as the application parent does.
func (m *Model) syncControls() { m.chrome.Props = m.Presentation() }

func (m *Model) applyTheme(name string) { screen.SetTheme(name); m.RefreshTheme() }
func (m *Model) testView() tea.View     { m.syncControls(); return m.chrome.Compose(m.View()) }

func (m *Model) bindControls() {
	m.chrome.Opening = m.OverlayOpened
	m.chrome.Closed = m.OverlayClosed
	m.chrome.Changed = func() { m.ControlsChanged(); m.syncControls() }
}

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
