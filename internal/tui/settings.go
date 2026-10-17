package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/speech"
	"github.com/keshon/tars/internal/tui/components/form"
	"github.com/keshon/tars/internal/tui/settings"
	"github.com/keshon/tars/internal/workspace"
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
	f.HasApplication, f.Chat = m.application != nil, m.chat != nil
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
	if f.Workspace != nil && f.Workspace.SampleCancel != nil {
		f.Error = "Stop the voice sample before applying settings"
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
	case settings.Preview:
		return m.previewSettingsSpeech()
	case settings.SwitchCategory:
		m.editSettings(intent.Section)
	}
	return nil
}
func (m *model) formView() string {
	f := m.dialog.form
	if f.Workspace == nil {
		rows, _, w, h := m.formRows()
		return renderBox(rows, w, h)
	}
	return f.View(m.settingsProps())
}
func (m *model) formMouse(msg tea.MouseMsg) tea.Cmd {
	intent, cmd := m.dialog.form.Mouse(msg, m.settingsProps())
	m.resizeFormEditor()
	return tea.Batch(cmd, m.handleSettingsIntent(intent))
}
func (m *model) editSettings(section string) {
	// Read before the teardown below. stopListening nils m.listening, so
	// anything that asks for the voice mode afterwards always gets
	// push-to-talk: a hands-free reader was downgraded by merely opening
	// this form, and the Application page reported itself as changed
	// because its Voice field no longer matched what it compared against.
	voice := m.liveVoiceMode()
	m.stopListening()
	if m.chat == nil && m.application == nil && section == "speech" {
		m.editAgentSpeech(voice)
		return
	}

	currentSettings := m.audio()
	if currentSettings == nil || currentSettings.Configure == nil {
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
		ws = &settingsWorkspace{Draft: settings.New(currentSettings.Profile), VoiceMode: voice}
	}
	ws.Section = section
	profile := ws.Draft.Base()
	preferences := config.Preferences{}
	if m.application != nil {
		preferences = m.application.Preferences
	}
	// Built from the recorded mode, not currentVoiceMode(): at this point
	// the new workspace is not attached yet, so the fallback would answer
	// with the post-teardown mode and the field would disagree with the
	// preference it is compared against.
	title, fields := settings.PageFields(profile, section, preferences, voice)

	if m.dialog != nil {
		m.closeDialog()
	}
	m.openForm(title, "Updates this session; history and draft are preserved.", "Apply", fields, func(_ map[string]string) error {
		m.storeSettingsPage()
		profile, err := ws.Draft.Resolve()
		if err != nil {
			return err
		}

		if m.chat == nil || m.settingsApplicationChanged() {
			return m.requestProfileChange(profile)
		}
		return m.applyChatSettings(profile, ws)
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
	m.openForm("Rename conversation", "Changes the title only; history stays intact.", "Rename", []formField{{Key: "title", Label: "Conversation title", Value: title, Multiline: false}}, func(values map[string]string) error {
		title := workspace.TitleLine(values["title"])
		if title == "" {
			return fmt.Errorf("title cannot be blank")
		}
		if err := workspace.WriteSessionTitle(dir, title); err != nil {
			return err
		}
		m.updateSessionTitle(dir, title)
		m.refreshNavigator()
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
	m.openForm("Delete conversation", "Permanently removes this saved conversation.", "Delete", nil, func(map[string]string) error {
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		m.newChat()
		m.refreshNavigator()
		return nil
	})
	m.dialog.form.Destructive = true
	// Destructive confirmation starts on Cancel.
	m.focusForm(1)
}
func (m *model) inspectConnection() {
	m.openDialog("Model connection", []string{"Model: " + nonEmpty(m.modelName), "Protocol: " + nonEmpty(m.backendKind), "Endpoint: " + nonEmpty(m.backendURL), fmt.Sprintf("Context: %d tokens · response: %d tokens", m.limit, m.responseCap), "Coding connection changes currently require restarting the application."})
}

func (m *model) inspectChatPrompt() {
	if m.audio() == nil {
		return
	}
	s := m.audio().Settings
	selected := ""
	if m.audio().Profile.Conversation != nil {
		selected = m.audio().Profile.Conversation.PromptPreset
	}
	preset, _ := prompts.ResponsePreset(selected)
	// The greeting and the dialogue examples come from a character card,
	// and a card is Chat-only. Offering an Agent a preview of two
	// headings it can never have described the wrong prompt.
	greeting, greetingLabel := s.Greeting, "Opening assistant greeting"
	if m.chat == nil {
		greeting, greetingLabel = "", "Opening assistant greeting (Chat only: this session takes no character)"
	}
	lines := []string{"Prompt preset: " + preset.Name, "Preset adds response style; a character card supplies identity in Chat.", "Leading system instruction", s.System, greetingLabel, greeting, "Post-history system directive", s.PostHistory, "This is the compiled prompt; saved conversation turns are inserted between greeting and post-history."}
	if m.chat == nil {
		lines = append([]string{"Agent keeps its Plan/Act operational instructions and adds this profile identity and response style."}, lines...)
	}
	for _, example := range s.Examples {
		lines = append(lines, string(example.Role)+": "+example.Content)
	}
	if m.voiceEnabled {
		lines = append(lines, speech.DeliveryPrompt(m.runtime().Voice))
	}
	lines = append(lines, m.audio().Warnings...)
	m.openDialog("Effective conversation prompt", lines)
}

func (m *model) resizeFormEditor() {
	if m.dialog == nil || m.dialog.form == nil {
		return
	}
	f := m.dialog.form
	f.Editor.SetWidth(max(min(m.transcript.Viewport.Width()-6, 72), 12))
	height := 1
	if f.Index < len(f.Fields) && f.Fields[f.Index].Multiline {
		height = min(3, max(m.transcript.Viewport.Height()-10, 1))
	}
	f.Editor.SetHeight(height)
}

func (m *model) formOptions(key string) []string { return m.dialog.form.Options(key) }
func (m *model) cycleFormChoice()                { m.dialog.form.CycleChoice() }

var formChoices = settings.Choices
var suggestedBackendURL = settings.SuggestedBackendURL
