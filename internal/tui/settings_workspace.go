package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
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
func (m *model) settingsDimensions() (int, int) {
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.Workspace != nil {
		return max(m.termW-2, 1), max(m.termH-m.actionBarRows()-1, 3)
	}
	return m.transcript.Viewport.Width(), m.transcript.Viewport.Height()
}

func (m *model) settingsDirty() bool {
	if m.dialog == nil || m.dialog.form == nil {
		return true
	}
	return m.dialog.form.Dirty()
}
func (m *model) openSettingsPicker() tea.Cmd { return m.dialog.form.OpenPicker(m.settingsProps()) }
func (m *model) acceptSettingsScan(msg settingsScanMsg) {
	if m.dialog != nil && m.dialog.form != nil {
		m.dialog.form.AcceptScan(msg)
	}
}
func (m *model) pickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m, m.dialog.form.PickerKey(msg, m.settingsProps())
}
func (m *model) scanPickerDirectory(root string) tea.Cmd { return m.dialog.form.ScanDirectory(root) }
func (m *model) settingsProps() settings.Props {
	w, h := m.settingsDimensions()
	return settings.Props{Width: w, Height: h, Title: m.dialog.title, Left: m.conversationOffset(), Top: paneTop, Styles: settings.Styles{Muted: m.styles.dim, Focus: m.styles.gate, Error: m.styles.err, Selected: m.styles.hunk}, SearchStyles: textInputStyles(textinput.New().Styles())}
}

var choiceLabel = settings.ChoiceLabel
var remoteModelChoices = settings.RemoteModelChoices
