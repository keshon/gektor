package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/prompts"
	"github.com/keshon/gektor/internal/providers"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/screen"
	"github.com/keshon/gektor/internal/tui/settings"
)

type formField = form.Field

type settingsForm = screen.FormState
type formHit = form.Hit

func (m *Model) updateFormInput(msg tea.Msg) tea.Cmd {
	m.syncControls()
	return m.chrome.UpdateFormInput(msg)
}

func (m *Model) openForm(title, hint, applyLabel string, fields []formField, apply func(map[string]string) error) {
	m.syncControls()
	m.chrome.OpenForm(title, hint, applyLabel, fields, apply)
	m.chrome.Dialog.Form.SaveCopy = m.saveSettingsCopy
}
func (m *Model) focusForm(index int) {
	m.syncControls()
	m.chrome.FocusForm(index)
}

// openRemoval keeps contextual read-only fields visible and starts on Cancel.
func (m *Model) openRemoval(title, hint, label string, fields []formField, apply func(map[string]string) error) {
	m.syncControls()
	m.chrome.OpenRemoval(title, hint, label, fields, apply)
	m.chrome.Dialog.Form.SaveCopy = m.saveSettingsCopy
}
func (m *Model) applyForm() {
	m.syncControls()
	m.chrome.ApplyForm()
}
func (m *Model) formKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.syncControls()
	return m, m.chrome.FormKey(msg)
}
func (m *Model) handleSettingsIntent(intent settings.Intent) tea.Cmd {
	m.syncControls()
	return m.chrome.HandleIntent(intent)
}

// formView draws the open form. Every form goes through the one path in the
// settings package, which frames and centres it - the section editor, the
// confirmation and the file chooser are the same surface with different rows,
// and the caller used to have a second way of drawing the first two.
func (m *Model) formView() string {
	m.syncControls()
	return m.chrome.FormView()
}
func (m *Model) formMouse(msg tea.MouseMsg) tea.Cmd {
	m.syncControls()
	return m.chrome.FormMouse(msg)
}
func (m *Model) editSettings(section string) {
	currentSettings := m.config
	if currentSettings == nil {
		return
	}
	var ws *settingsWorkspace
	if m.chrome.Dialog != nil && m.chrome.Dialog.Form != nil {
		ws = m.chrome.Dialog.Form.EditSession
		if ws != nil {
			m.storeSettingsPage()
		}
	}
	if ws == nil {
		ws = &settingsWorkspace{Draft: settings.New(currentSettings.Profile), PersistCurrent: m.application != nil && m.application.Temporary}
	}
	ws.Section = section
	ws.AccountLabel = m.accountLabel
	profile := ws.Draft.Base()
	if section != "model" {
		if modelFields, ok := ws.Draft.Fields("model"); ok {
			for _, field := range modelFields {
				if field.Key == "kind" {
					profile.Provider.Kind = field.Value
				}
			}
		}
	}
	preferences := config.Preferences{}
	if m.application != nil {
		preferences = m.application.Preferences
	}
	title, fields := settings.PageFields(profile, section, preferences)

	if m.chrome.Dialog != nil {
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
	m.chrome.Dialog.Form.EditSession = ws
	m.chrome.Dialog.Form.DiscoverModels = remoteModelChoices
	m.chrome.Dialog.Form.DiscoverReasoning = remoteReasoningChoices
	saved := ws.Draft.Visit(section, fields)
	m.chrome.Dialog.Form.Fields = saved
	m.chrome.Dialog.Form.Index = len(saved)
	m.focusForm(0)
	m.chrome.Dialog.Title = "Settings"
}

func (m *Model) renameActive() {
	dir, title := filepath.Dir(m.stateFile), m.activeSessionTitle()
	m.openForm("Rename session", "Changes the title only; history stays intact.", "Rename", []formField{{Key: "title", Label: "Session title", Value: title, Multiline: false}}, func(values map[string]string) error {
		title := conversation.TitleLine(values["title"])
		if title == "" {
			return fmt.Errorf("title cannot be blank")
		}
		if err := conversation.WriteSessionTitle(dir, title); err != nil {
			return err
		}
		m.updateSessionTitle(dir, title)
		m.refreshSessions()
		return nil
	})
}
func (m *Model) deleteActive() {
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
	m.chrome.Dialog.Form.Destructive = true
	// Destructive confirmation starts on Cancel.
	m.focusForm(1)
}
func (m *Model) inspectConnection() {
	m.openDialog("Model connection", []string{"Model: " + nonEmpty(m.modelName), "Protocol: " + nonEmpty(m.backendKind), "Endpoint: " + nonEmpty(m.backendURL), fmt.Sprintf("Context: %d tokens · response: %d tokens", m.limit, m.responseCap), "Coding connection changes currently require restarting the application."})
}

func (m *Model) inspectAgentInstructions() {
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
	m.openDialog("Agent instructions preview", lines)
}

func (m *Model) resizeFormEditor() {
	m.syncControls()
	m.chrome.ResizeFormEditor()
}

func remoteModelChoices(kind, endpoint, keyEnv string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	items, err := app.DiscoverModels(ctx, app.BackendOptions{Kind: kind, URL: endpoint, APIKeyEnv: keyEnv})
	if err != nil {
		return nil, fmt.Errorf("%w; Esc returns to manual entry", err)
	}
	return items, nil
}

func remoteReasoningChoices(kind, endpoint, model string) (providers.ReasoningOptions, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return app.DiscoverReasoning(ctx, app.BackendOptions{Kind: kind, URL: endpoint, Model: model})
}

func (m *Model) confirmCompact() {
	m.openForm("Compact history", "Replaces older tool details with a short record and keeps recent steps. This changes the saved history used by the model.", "Compact", nil, func(map[string]string) error {
		_, cmd := m.compactNow()
		if cmd != nil {
			m.pendingCommands = append(m.pendingCommands, cmd)
		}
		return nil
	})
	m.focusForm(1)
}
