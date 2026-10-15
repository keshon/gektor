package tui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/keshon/tars/internal/character"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/speech"
	"github.com/keshon/tars/internal/tui/components/form"
	"github.com/keshon/tars/internal/workspace"
)

type formField = form.Field

type settingsForm struct {
	form.Model

	error, hint, applyLabel string
	apply                   func(map[string]string) error
	destructive             bool
	choices                 map[string][]string
	scannedRoot             string
	greetingPath            string
	workspace               *settingsWorkspace
	picker                  *settingsPicker
}
type formHit = form.Hit

func (m *model) openForm(title, hint, applyLabel string, fields []formField, apply func(map[string]string) error) {
	m.openDialog(title, nil)
	f := &settingsForm{Model: form.New(fields, newInput().Model), hint: hint, applyLabel: applyLabel, apply: apply}
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
	if f.workspace != nil && !m.settingsDirty() {
		return
	}
	if f.workspace != nil && f.workspace.sampleCancel != nil {
		f.error = "Stop the voice sample before applying settings"
		return
	}
	values := f.Values()
	if err := f.apply(values); err != nil {
		f.error = err.Error()
		return
	}
	m.closeDialog()
}
func (m *model) formKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.dialog.form
	if f.picker != nil {
		return m.pickerKey(msg)
	}
	if f.workspace != nil {
		if msg.String() == "ctrl+d" && f.workspace.section == "profiles" {
			m.saveSettingsCopy()
			return m, nil
		}
		if msg.String() == "ctrl+p" && f.workspace.section == "speech" {
			return m, m.previewSettingsSpeech()
		}
		if msg.String() == "f6" {
			f.workspace.nav = !f.workspace.nav
			return m, nil
		}
		if msg.String() == "f4" {
			f.workspace.advanced = !f.workspace.advanced
			m.focusForm(m.firstVisibleField())
			return m, nil
		}
		if f.workspace.nav {
			return m.settingsNavKey(msg)
		}
	}

	choice := f.Index < len(f.Fields) && (len(m.formOptions(f.Fields[f.Index].Key)) > 0 || pickerControl(f.Fields[f.Index].Key))
	intent, cmd := f.Update(msg, m.settingsFieldVisible, choice, f.destructive)
	m.resizeFormEditor()
	switch intent {
	case form.Apply:
		m.applyForm()
	case form.Cancel:
		m.closeDialog()
	case form.Pick:
		return m, m.openSettingsPicker()
	}
	return m, cmd
}

func (m *model) formStyles() form.Styles {
	return form.Styles{Muted: m.styles.dim, Focus: m.styles.gate, Error: m.styles.err}
}
func (m *model) plainFormRows() ([]string, []formHit, int, int) {
	f := m.dialog.form
	w, h := m.transcript.Viewport.Width(), m.transcript.Viewport.Height()
	choice := ""
	if f.Index < len(f.Fields) && len(m.formOptions(f.Fields[f.Index].Key)) > 0 {
		choice = choiceLabel(f.Fields[f.Index].Key, f.Editor.Value())
	}
	rows, hits := f.Rows(form.Props{Title: m.dialog.title, Hint: f.hint, Error: f.error, PrimaryLabel: f.applyLabel, Width: w, Height: h, Destructive: f.destructive, Choice: choice, Styles: m.formStyles()})
	return rows, hits, w, h
}
func (m *model) formView() string {
	rows, _, w, h := m.formRows()
	if m.dialog.form.workspace == nil {
		return renderBox(rows, w, h)
	}
	width := max(m.termW-2, 0)
	rail := m.dialog.form.picker == nil && w >= 76
	rule := strings.Repeat("─", width)
	if rail {
		rule = strings.Repeat("─", 16) + "┬" + strings.Repeat("─", max(width-17, 0))
	}
	out := []string{m.styles.dim.Render("╭" + rule + "╮")}
	for i := 0; i < max(h-2, 0); i++ {
		row := ""
		if i < len(rows) {
			row = rows[i]
		} else if rail {
			row = strings.Repeat(" ", 15) + m.styles.dim.Render("│ ")
		}
		out = append(out, m.styles.dim.Render("│ ")+cellLine(row, max(width-2, 0))+m.styles.dim.Render(" │"))
	}
	if rail {
		rule = strings.Replace(rule, "┬", "┴", 1)
	}
	out = append(out, m.styles.dim.Render("╰"+rule+"╯"))
	return strings.Join(out, "\n")
}
func (m *model) formMouse(msg tea.MouseMsg) tea.Cmd {
	if m.dialog.form.picker != nil && (msg.Mouse().Button == tea.MouseWheelUp || msg.Mouse().Button == tea.MouseWheelDown) {
		p := m.dialog.form.picker
		step := 1
		if msg.Mouse().Button == tea.MouseWheelUp {
			step = -1
		}
		p.Move(step, len(p.filtered), 1)
		return nil
	}
	if msg.Mouse().Button != tea.MouseLeft || !isMouseClick(msg) {
		return nil
	}
	rows, hits, w, h := m.formRows()
	bw := 0
	for _, row := range rows {
		bw = max(bw, lipgloss.Width(row))
	}
	x := m.conversationOffset() + max((w-bw-4)/2, 0)
	y := paneTop + max((h-len(rows)-2)/2, 0) + 1
	if m.dialog.form.workspace != nil {
		x = 0
		y = 2
	}
	mouse := msg.Mouse()
	if m.dialog.form.picker != nil {
		p := m.dialog.form.picker
		row := mouse.Y - y - 3
		capacity := max(h-10, 1)
		start := max(min(p.Cursor-capacity/2, len(p.filtered)-capacity), 0)
		if mouse.X >= x && mouse.X < x+bw+4 && row >= 0 && row < capacity && start+row < len(p.filtered) {
			p.Cursor = start + row
			_, cmd := m.pickerKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			return cmd
		}
		return nil
	}
	if m.dialog.form.workspace != nil && w >= 76 && mouse.X >= x+1 && mouse.X < x+17 {
		row := mouse.Y - y
		if row >= 0 && row < len(m.availableSettingsCategories()) {
			category := m.availableSettingsCategories()[row].id
			if category != "speech" || m.hasSettingsSpeech() {
				if category != m.dialog.form.workspace.section {
					m.editSettings(category)
				}
			}
		}
		if row >= 0 && row < len(m.availableSettingsCategories()) {
			return nil
		}
	}
	if mouse.X < x || mouse.X >= x+bw+4 {
		return nil
	}
	for _, hit := range hits {
		if mouse.Y >= y+hit.Y0 && mouse.Y < y+hit.Y1 {
			if hit.X1 > 0 {
				contentX := x + 2
				if m.dialog.form.workspace != nil && w >= 76 {
					contentX += 17
				}
				if mouse.X < contentX+hit.X0 || mouse.X >= contentX+hit.X1 {
					continue
				}
			}
			m.focusForm(hit.Index)
			if hit.Index < len(m.dialog.form.Fields) && mouse.Y > y+hit.Y0 && (len(m.formOptions(m.dialog.form.Fields[hit.Index].Key)) > 0 || pickerControl(m.dialog.form.Fields[hit.Index].Key)) {
				return m.openSettingsPicker()
			}
			if hit.Index == len(m.dialog.form.Fields) {
				m.applyForm()
			} else if hit.Index == len(m.dialog.form.Fields)+1 {
				m.closeDialog()
			}
			break
		}
	}
	return nil
}
func (m *model) editSettings(section string) {
	m.stopListening()
	if m.chat == nil && m.application == nil && section == "speech" {
		m.editAgentSpeech()
		return
	}

	settings := m.audio()
	if settings == nil || settings.Configure == nil {
		return
	}
	var ws *settingsWorkspace
	if m.dialog != nil && m.dialog.form != nil {
		ws = m.dialog.form.workspace
		if ws != nil {
			m.storeSettingsPage()
		}
	}
	if ws == nil {
		ws = &settingsWorkspace{cache: map[string][]formField{}, original: map[string][]formField{}}
	}
	ws.section = section
	profile := settings.Profile
	if ws.loaded != nil {
		profile = *ws.loaded
	}
	conversation := config.Conversation{UserName: "User"}
	if profile.Conversation != nil {
		conversation = *profile.Conversation
	}
	profile.Conversation = &conversation
	if profile.Speech != nil {
		copy := *profile.Speech
		profile.Speech = &copy
	}
	fields := []formField{}
	add := func(key, label, value string, multi bool) {
		fields = append(fields, formField{Key: key, Label: label, Value: value, Multiline: multi})
	}
	title := ""
	switch section {
	case "application":
		title = "Application"
		p := m.application.Preferences
		add("app-mode", "Mode", p.Mode, false)
		add("app-workspace", "Workspace", p.Workspace, false)
		add("app-voice", "Voice", m.currentVoiceMode(), false)
	case "profiles":
		title = "Profiles"
		add("load-profile", "Load setup from profile", "", false)
		add("profile-name", "Save current setup as (Ctrl+D)", "custom", false)
	case "character":
		title = "Character"
		add("character", "Character card (PNG/JSON; blank removes card)", conversation.Character, false)
		add("name", "Assistant name (without a card)", conversation.Name, false)
		add("greeting", "Opening greeting", strconv.Itoa(conversation.GreetingIndex), false)
	case "you":
		title = "You"
		add("user", "Your name", conversation.UserName, false)
		add("persona", "Your persona description", conversation.UserPersona, true)
	case "generation":
		title = "Generation"
		add("temperature", "Temperature (0 = default 0.4)", strconv.FormatFloat(profile.Generation.Temperature, 'f', -1, 64), false)
		add("max", "Response budget (tokens)", strconv.Itoa(profile.Generation.MaxTokens), false)
		add("context", "Context window (tokens)", strconv.Itoa(profile.Generation.ContextLimit), false)
	case "prompts":
		title = "Conversation prompts"
		preset, _ := prompts.ResponsePreset(conversation.PromptPreset)
		add("prompt-preset", "Prompt preset", preset.ID, false)
		add("system", "Base system prompt", conversation.SystemPrompt, true)
		add("post", "Base post-history directive", conversation.PostHistory, true)
		main, post := true, true
		if conversation.PreferCharacterPrompt != nil {
			main = *conversation.PreferCharacterPrompt
		}
		if conversation.PreferCharacterPostHistory != nil {
			post = *conversation.PreferCharacterPostHistory
		}
		add("prefer-main", "Use card main prompt (true/false)", strconv.FormatBool(main), false)
		add("prefer-post", "Use card post-history (true/false)", strconv.FormatBool(post), false)
	case "speech":
		title = "Speech services"
		add("microphone", "Microphone (Windows device name)", profile.Speech.Microphone, false)
		add("variant", "Chatterbox variant", profile.Speech.Speaking.Variant, false)
		add("speech-model", "Speech model directory", profile.Speech.Speaking.Model, false)
		add("seconds", "Maximum recording seconds", strconv.Itoa(profile.Speech.MaxSeconds), false)
		mode := profile.Speech.VAD
		if mode == "" {
			mode = "silero"
		}
		add("vad", "Speech detector", mode, false)
		add("mic-start", "Speech onset (ms; 0 = 200)", strconv.Itoa(profile.Speech.MicStartMS), false)
		add("mic-end", "End silence (ms; 0 = 1100)", strconv.Itoa(profile.Speech.MicEndMS), false)
		add("mic-threshold", "Energy threshold (0 = adaptive)", strconv.FormatFloat(profile.Speech.MicThreshold, 'f', -1, 64), false)
	case "model":
		if profile.Provider.Runtime == "" {
			exe, err := os.Executable()
			if err == nil {
				profile.Provider.Runtime = filepath.Join(filepath.Dir(exe), "assets", "runtimes", "llamacpp", "llama-server.exe")
			}
		}
		title = "Model"
		add("ownership", "Thinking service (external/managed)", profile.Provider.Ownership, false)
		add("runtime", "llama-server executable", profile.Provider.Runtime, false)
		add("model-root", "Model directory (optional; scans nested folders)", profile.Provider.ModelRoot, false)
		add("model-file", "GGUF model file (any directory)", profile.Provider.ModelFile, false)
		add("gpu-layers", "GPU layers (0 CPU; 99 all layers)", strconv.Itoa(profile.Provider.GPULayers), false)
		add("kind", "Protocol (llama/kobold/openai)", profile.Provider.Kind, false)
		add("endpoint", "Backend URL", profile.Provider.URL, false)
		add("model", "Model identifier", profile.Provider.Model, false)
		add("key-env", "API key environment variable", profile.Provider.APIKeyEnv, false)
	}
	if m.dialog != nil {
		m.closeDialog()
	}
	m.openForm(title, "Updates this session; history and draft are preserved.", "Apply", fields, func(values map[string]string) error {
		if m.dialog.form.workspace != nil {
			ws := m.dialog.form.workspace
			if ws.loaded != nil {
				profile = cloneSettingsProfile(*ws.loaded)
			}
			ws.cache[section] = append([]formField(nil), m.dialog.form.Fields...)
			for category, fields := range ws.cache {
				if reflect.DeepEqual(fields, ws.original[category]) {
					continue
				}
				v := map[string]string{}
				for _, field := range fields {
					v[field.Key] = field.Value
				}
				if category == section {
					v = values
				}
				if category == "application" {
					continue
				}
				if err := config.ApplySection(&profile, category, v); err != nil {
					return fmt.Errorf("%s: %w", category, err)
				}
			}
		} else if err := config.ApplySection(&profile, section, values); err != nil {
			return err
		}

		if m.chat == nil || m.settingsApplicationChanged() {
			return m.requestProfileChange(profile)
		}
		next, err := m.chat.config.Configure(profile)
		if err != nil {
			return err
		}
		// No conversation exists until the first user turn. Keep that lazy state:
		// constructing one here would leave it without an archive path.
		var updated *chat.Conversation
		if m.chat.current != nil {
			updated, err = m.chat.current.Reconfigured(next.Client, next.Settings)
			if err != nil {
				return err
			}
		}
		if err := m.saveCustomSettings(profile); err != nil {
			if c, ok := next.Client.(interface{ Close() }); ok {
				c.Close()
			}
			return err
		}
		if _, err := os.Stat(m.stateFile); err == nil && updated != nil {
			if err := updated.Save(m.stateFile); err != nil {
				if c, ok := next.Client.(interface{ Close() }); ok {
					c.Close()
				}
				return err
			}
		}
		if !reflect.DeepEqual(profile.Speech, m.chat.config.Profile.Speech) && m.voice != nil {
			m.voice.Close()
			m.voice = nil
			m.voiceEnabled = false
		}
		if profile.Provider == m.chat.config.Profile.Provider && profile.Generation.ContextLimit == m.chat.config.Profile.Generation.ContextLimit {
			if c, ok := next.Client.(interface{ Close() }); ok {
				c.Close()
			}
			next.Client = m.chat.config.Client
			if m.chat.current != nil {
				updated, _ = m.chat.current.Reconfigured(next.Client, next.Settings)
			}
		} else if c, ok := m.chat.config.Client.(interface{ Close() }); ok {
			c.Close()
		}
		m.chat.current = updated
		m.chat.config = next
		m.backendKind = profile.Provider.Kind
		m.backendURL = safeEndpoint(profile.Provider.URL)
		m.modelName = profile.Provider.Model
		m.limit = next.Settings.ContextLimit
		m.responseCap = next.Settings.MaxTokens
		m.streaming = next.Stream
		previousSource := ""
		if m.application != nil && ws.loadedPath != "" {
			previousSource = m.application.Preferences.ProfilePath
			m.application.Preferences.ProfilePath = ws.loadedPath
		}
		save := m.saveSessionSettings
		if ws.autoSaved && m.application != nil && !m.application.Temporary {
			save = m.saveApplication
		}
		if err := save(); err != nil {
			if m.application != nil && ws.loadedPath != "" {
				m.application.Preferences.ProfilePath = previousSource
			}
			return err
		}
		message := "Settings applied to this session"
		if ws.autoSaved {
			message += " · saved " + filepath.Base(ws.loadedPath)
		}
		m.appendBlock(markerBlock(message))

		for _, warning := range next.Warnings {
			m.appendBlock(markerBlock("Character warning: " + warning))
		}
		return nil
	})
	m.dialog.form.workspace = ws
	if _, ok := ws.original[section]; !ok {
		ws.original[section] = append([]formField(nil), fields...)
	}
	if saved, ok := ws.cache[section]; ok {
		m.dialog.form.Fields = append([]formField(nil), saved...)
		m.dialog.form.Index = len(saved)
		m.focusForm(0)
	}
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
	m.dialog.form.destructive = true
	// Destructive confirmation starts on Cancel.
	m.focusForm(1)
}
func (m *model) inspectConnection() {
	m.openDialog("Model connection", []string{"Model: " + nonEmpty(m.modelName), "Protocol: " + nonEmpty(m.backendKind), "Endpoint: " + nonEmpty(m.backendURL), fmt.Sprintf("Context: %d tokens · response: %d tokens", m.limit, m.responseCap), "Coding connection changes currently require restarting the application."})
}

func (m *model) saveChatProfile() {
	profile := cloneSettingsProfile(m.chat.config.Profile)
	m.openForm("Save configuration as profile", "Creates a new file; existing profiles are never overwritten.", "Save profile", []formField{{Key: "path", Label: "New profile file", Value: "profiles/chat-custom.json", Multiline: false}}, func(values map[string]string) error {
		path := strings.TrimSpace(values["path"])
		if path == "" {
			return fmt.Errorf("profile path is required")
		}
		if err := config.SaveCopy(profile, path); err != nil {
			return err
		}
		m.appendBlock(markerBlock("Profile saved: " + path))
		return nil
	})
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
	lines := []string{"Prompt preset: " + preset.Name, "Preset adds response style; the card supplies identity. Agent keeps its operational instructions.", "Leading system instruction", s.System, "Opening assistant greeting", s.Greeting, "Post-history system directive", s.PostHistory, "This is the compiled prompt; saved conversation turns are inserted between greeting and post-history."}
	if m.chat == nil {
		lines = append([]string{"Agent keeps its Plan/Act operational instructions and adds this character identity."}, lines...)
	}
	for _, example := range s.Examples {
		lines = append(lines, string(example.Role)+": "+example.Content)
	}
	if m.voiceEnabled {
		lines = append(lines, speech.DeliveryPrompt(m.voice))
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

func (m *model) formOptions(key string) []string {
	if key == "greeting" && m.dialog != nil && m.dialog.form != nil {
		f := m.dialog.form
		path := ""
		for _, field := range f.Fields {
			if field.Key == "character" {
				path = field.Value
			}
		}
		if f.choices == nil {
			f.choices = map[string][]string{}
		}
		if path != f.greetingPath || len(f.choices[key]) == 0 {
			f.greetingPath = path
			f.choices[key] = []string{"0"}
			if card, err := character.Load(path); err == nil {
				for i := range card.AlternateGreetings {
					f.choices[key] = append(f.choices[key], strconv.Itoa(i+1))
				}
			}
		}
	}

	if m.dialog != nil && m.dialog.form != nil {
		if choices := m.dialog.form.choices[key]; len(choices) > 0 {
			return choices
		}
	}
	return formChoices(key)
}

func formChoices(key string) []string {
	switch key {
	case "prompt-preset":
		ids := []string{}
		for _, preset := range prompts.Presets() {
			ids = append(ids, preset.ID)
		}
		return ids
	case "prefer-main", "prefer-post":
		return []string{"true", "false"}
	case "app-mode":
		return []string{"chat", "agent"}
	case "app-voice":
		return []string{"off", "ptt", "live"}
	case "vad":
		return []string{"silero", "energy"}
	case "ownership":
		return []string{"external", "managed"}
	case "kind":
		return []string{"llama", "kobold", "openai"}
	case "variant":
		return []string{"nano", "turbo"}
	}
	return nil
}

// suggestedBackendURL changes only the previous protocol's local default.
// OpenAI denotes a compatible API, so its suggestion stays local.
func suggestedBackendURL(endpoint, previous, next string) string {
	ports := map[string]string{"llama": "8080", "kobold": "5001", "openai": "8080"}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return endpoint
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" {
		return endpoint
	}
	path := strings.TrimSuffix(u.Path, "/")
	if u.Port() != ports[previous] || (path != "" && !(previous == "openai" && path == "/v1")) {
		return endpoint
	}
	if ports[next] == "" {
		return endpoint
	}
	u.Host = host + ":" + ports[next]
	u.Path = ""
	if next == "openai" {
		u.Path = "/v1"
	}
	return u.String()
}

func (m *model) cycleFormChoice() {
	f := m.dialog.form
	options := m.formOptions(f.Fields[f.Index].Key)
	if len(options) == 0 {
		return
	}
	previous := f.Editor.Value()
	next := options[0]
	for i, value := range options {
		if value == previous {
			next = options[(i+1)%len(options)]
			break
		}
	}
	if f.Fields[f.Index].Key == "kind" {
		for i := range f.Fields {
			if f.Fields[i].Key == "endpoint" {
				f.Fields[i].Value = suggestedBackendURL(f.Fields[i].Value, previous, next)
			}
		}
	}
	f.Editor.SetValue(next)
}

// formActions shares button geometry between compact dialogs and settings.
func (m *model) formActions(width, y int, primary string, enabled bool) (string, []formHit) {
	return m.dialog.form.Actions(width, y, primary, enabled, m.formStyles())
}
