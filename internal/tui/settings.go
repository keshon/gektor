package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/settings"
	"github.com/keshon/gektor/internal/workspace"
)

type formField = form.Field

type settingsForm struct {
	settings.Model
	apply func(map[string]string) error
}
type formHit = form.Hit

func (m *model) updateFormInput(msg tea.Msg) tea.Cmd {
	if m.dialog == nil || m.dialog.form == nil {
		return nil
	}
	return m.dialog.form.UpdateInput(msg)
}

func (m *model) openForm(title, hint, applyLabel string, fields []formField, apply func(map[string]string) error) {
	m.openDialog(title, nil)
	f := &settingsForm{Model: settings.NewModel(fields, newInput().Model), apply: apply}
	f.Hint, f.ApplyLabel = hint, applyLabel
	f.HasApplication = m.application != nil
	f.Editor.SetVirtualCursor(true)
	f.Editor.DynamicHeight = false
	f.Editor.Prompt = ""
	f.Editor.CharLimit = 65536
	m.dialog.form = f
	m.focusForm(0)
}
func (m *model) focusForm(index int) {
	f := m.dialog.form
	f.Focus(index, m.settingsFieldVisible)

	m.resizeFormEditor()
}
func (m *model) applyForm() {
	f := m.dialog.form
	if f.Workspace != nil && !m.settingsDirty() {
		return
	}
	values := f.Values()
	if err := f.apply(values); err != nil {
		f.Error = err.Error()
		return
	}
	m.closeDialog()
}
func (m *model) formKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	intent, cmd := m.dialog.form.UpdateKey(msg, m.settingsProps())
	m.resizeFormEditor()
	return m, tea.Batch(cmd, m.handleSettingsIntent(intent))
}
func (m *model) handleSettingsIntent(intent settings.Intent) tea.Cmd {
	switch intent.Action {
	case settings.Apply:
		m.applyForm()
	case settings.Cancel:
		m.closeDialog()
	case settings.SaveCopy:
		m.saveSettingsCopy()
	}
	return nil
}

// formView draws the open form. Every form goes through the one path in the
// settings package, which frames and centres it - the section editor, the
// confirmation and the file chooser are the same surface with different rows,
// and the caller used to have a second way of drawing the first two.
func (m *model) formView() string {
	return m.dialog.form.View(m.settingsProps())
}
func (m *model) formMouse(msg tea.MouseMsg) tea.Cmd {
	intent, cmd := m.dialog.form.Mouse(msg, m.settingsProps())
	m.resizeFormEditor()
	return tea.Batch(cmd, m.handleSettingsIntent(intent))
}
func (m *model) editSettings(section string) {
	currentSettings := m.config
	if currentSettings == nil {
		return
	}
	var ws *settingsWorkspace
	if m.dialog != nil && m.dialog.form != nil {
		ws = m.dialog.form.Workspace
		if ws != nil {
			m.storeSettingsPage()
		}
	}
	if ws == nil {
		ws = &settingsWorkspace{Draft: settings.New(currentSettings.Profile)}
	}
	ws.Section = section
	profile := ws.Draft.Base()
	preferences := config.Preferences{}
	if m.application != nil {
		preferences = m.application.Preferences
	}
	title, fields := settings.PageFields(profile, section, preferences)

	if m.dialog != nil {
		m.closeDialog()
	}
	m.openForm(title, "GEKTOR restarts to apply these. The conversation and draft are preserved.", "Apply", fields, func(_ map[string]string) error {
		m.storeSettingsPage()
		profile, err := ws.Draft.Resolve()
		if err != nil {
			return err
		}

		// Applying settings restarts the launch so the new profile is composed
		// into fresh services. Nothing is applied in place: the session that
		// reads the old one must not keep running against the new.
		return m.requestProfileChange(profile)
	})
	m.dialog.form.Workspace = ws
	saved := ws.Draft.Visit(section, fields)
	m.dialog.form.Fields = saved
	m.dialog.form.Index = len(saved)
	m.focusForm(0)
	m.dialog.title = "Settings"
}

func (m *model) renameActive() {
	dir, title := filepath.Dir(m.stateFile), m.activeSessionTitle()
	m.openForm("Rename session", "Changes the title only; history stays intact.", "Rename", []formField{{Key: "title", Label: "Session title", Value: title, Multiline: false}}, func(values map[string]string) error {
		title := workspace.TitleLine(values["title"])
		if title == "" {
			return fmt.Errorf("title cannot be blank")
		}
		if err := workspace.WriteSessionTitle(dir, title); err != nil {
			return err
		}
		m.updateSessionTitle(dir, title)
		m.refreshSessions()
		return nil
	})
}
func (m *model) deleteActive() {
	target, err := filepath.Abs(filepath.Dir(m.stateFile))
	root, rootErr := filepath.Abs(m.sessionRoot())
	if err != nil || rootErr != nil || !strings.EqualFold(filepath.Dir(target), root) {
		m.openDialog("Cannot delete", []string{"Session is outside the configured session store."})
		return
	}
	m.openForm("Delete session", "Permanently removes this saved session.", "Delete", nil, func(map[string]string) error {
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		m.newTask()
		m.refreshSessions()
		return nil
	})
	m.dialog.form.Destructive = true
	// Destructive confirmation starts on Cancel.
	m.focusForm(1)
}
func (m *model) inspectConnection() {
	m.openDialog("Model connection", []string{"Model: " + nonEmpty(m.modelName), "Protocol: " + nonEmpty(m.backendKind), "Endpoint: " + nonEmpty(m.backendURL), fmt.Sprintf("Context: %d tokens · response: %d tokens", m.limit, m.responseCap), "Coding connection changes currently require restarting the application."})
}

func (m *model) inspectPrompt() {
	if m.config == nil {
		return
	}
	s := m.config.Identity
	selected := ""
	if m.config.Profile.Identity != nil {
		selected = m.config.Profile.Identity.PromptPreset
	}
	preset, _ := prompts.ResponsePreset(selected)
	lines := []string{
		"Prompt preset: " + preset.Name,
		"Preset adds response style to the profile's own identity.",
		"Leading identity instruction",
		s.System,
		"Post-history system directive",
		s.PostHistory,
		"This is the identity the profile contributes; the agent's Plan/Act operational instructions are added on top and are not shown here.",
	}
	lines = append(lines, m.config.Warnings...)
	m.openDialog("Effective identity prompt", lines)
}

func (m *model) resizeFormEditor() {
	if m.dialog == nil || m.dialog.form == nil {
		return
	}
	f := m.dialog.form
	f.Editor.SetWidth(max(popup.ModalWidth(m.transcript.Viewport.Width())-4, 1))
	height := 1
	if f.Index < len(f.Fields) && f.Fields[f.Index].Multiline {
		height = min(3, max(m.transcript.Viewport.Height()-10, 1))
	}
	f.Editor.SetHeight(height)
}
