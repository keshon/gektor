package agent

import (
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/pkg/tui/screen"
)

// Commander transcript origin used by its layout assertions.
const paneTop = 4

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

func (m *Model) applyAppearance(appearance config.Appearance) {
	screen.SetAppearance(screen.Appearance{
		Name:            currentPalette().Name,
		Caret:           appearance.Caret,
		StatusAnimation: appearance.StatusAnimation,
		Modern:          appearance.Style == "modern",
		Outlined:        appearance.Frame == "outlined",
	})
	m.ApplyHistoryAppearance(appearance)
}

func (m *Model) setMinimal(minimal bool) {
	appearance := currentPalette().Appearance
	appearance.Outlined = !minimal
	screen.SetAppearance(appearance)
}

// Distance from the bottom to the last input row, including the toolbar.
func (m *Model) footerRows() int {
	layout := m.composerLayout()
	if layout.input.Height > 0 {
		return m.termH - layout.input.Y - layout.input.Height
	}
	if layout.note.Height > 0 {
		return m.termH - layout.note.Y - layout.note.Height
	}
	return m.actionBarRows() + m.promptOffset()
}
