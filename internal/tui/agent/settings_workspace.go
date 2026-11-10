package agent

import (
	"github.com/keshon/gektor/internal/tui/settings"
)

type settingsWorkspace = settings.EditSession
type settingsScanMsg = settings.ScanMsg

func (m *Model) storeSettingsPage() {
	m.chrome.Dialog.Form.StorePage()
}
func (m *Model) settingsFieldVisible(index int) bool {
	m.syncControls()
	return m.chrome.FieldVisible(index)
}
func (m *Model) formRows() ([]string, []formHit, int, int) {
	m.syncControls()
	return m.chrome.FormRows()
}

// settingsArea is the screen region the open form is drawn into: the
// transcript pane.
//
// It used to have a second answer for the settings editor, on the theory that
// a section needs more room than a confirmation. It does not - the pane is
// the same pane - and having two answers meant the form's renderer and its
// hit-test could be reading a different region than the one it was drawn into.
func (m *Model) settingsArea() (left, top, width, height int) {
	if m.externalControls {
		return m.chrome.Area()
	}
	return m.dialogArea()
}
func (m *Model) dialogArea() (left, top, width, height int) {
	return m.promptOffset(), paneTop, m.transcript.Viewport.Width(), m.transcript.Viewport.Height()
}

func (m *Model) settingsDirty() bool {
	if m.chrome.Dialog == nil || m.chrome.Dialog.Form == nil {
		return true
	}
	return m.chrome.Dialog.Form.Dirty()
}
func (m *Model) acceptSettingsScan(msg settingsScanMsg) {
	if m.chrome.Dialog != nil && m.chrome.Dialog.Form != nil {
		m.chrome.Dialog.Form.AcceptScan(msg)
	}
}
func (m *Model) settingsProps() settings.Props {
	m.syncControls()
	return m.chrome.SettingsProps()
}
