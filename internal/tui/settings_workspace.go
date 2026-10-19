package tui

import (
	"charm.land/bubbles/v2/textinput"
	"github.com/keshon/tars/internal/tui/settings"
)

type settingsWorkspace = settings.Workspace
type settingsScanMsg = settings.ScanMsg

func (m *model) storeSettingsPage() {
	m.dialog.form.StorePage()
}
func (m *model) settingsFieldVisible(index int) bool { return m.dialog.form.FieldVisible(index) }
func (m *model) formRows() ([]string, []formHit, int, int) {
	return m.dialog.form.Rows(m.settingsProps())
}

// settingsArea is the screen region the open form is drawn into: the
// transcript pane, wherever the sidebar puts it.
//
// It used to have a second answer for the settings editor, on the theory that
// a section needs more room than a confirmation. It does not - the pane is
// the same pane - and having two answers meant the form's renderer and its
// hit-test could be reading a different region than the one it was drawn into.
func (m *model) settingsArea() (left, top, width, height int) {
	return m.promptOffset(), paneTop, m.transcript.Viewport.Width(), m.transcript.Viewport.Height()
}

func (m *model) settingsDirty() bool {
	if m.dialog == nil || m.dialog.form == nil {
		return true
	}
	return m.dialog.form.Dirty()
}
func (m *model) acceptSettingsScan(msg settingsScanMsg) {
	if m.dialog != nil && m.dialog.form != nil {
		m.dialog.form.AcceptScan(msg)
	}
}
func (m *model) settingsProps() settings.Props {
	left, top, width, height := m.settingsArea()
	// The frame palette is the same one every other overlay uses, handed
	// over rather than re-derived so no overlay invents its own.
	return settings.Props{
		Width: width, Height: height, Title: m.dialog.title,
		Left: left, Top: top,
		Styles:       settings.Styles{Muted: m.styles.dim, Focus: m.styles.gate, Error: m.styles.err, Selected: m.styles.hunk},
		Surface:      popupSurface(),
		SearchStyles: textInputStyles(textinput.New().Styles()),
	}
}
