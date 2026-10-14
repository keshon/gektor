package tui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/keshon/tars/internal/chat"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/workspace"
)

type formField struct {
	key, label, value string
	multiline         bool
}
type settingsForm struct {
	fields                  []formField
	index                   int
	editor                  textarea.Model
	error, hint, applyLabel string
	apply                   func(map[string]string) error
	destructive             bool
	choices                 map[string][]string
	scannedRoot             string
	workspace               *settingsWorkspace
	picker                  *settingsPicker
}
type formHit struct{ index, y0, y1, x0, x1 int }

func (m *model) openForm(title, hint, applyLabel string, fields []formField, apply func(map[string]string) error) {
	m.openDialog(title, nil)
	f := &settingsForm{fields: fields, index: len(fields), hint: hint, applyLabel: applyLabel, apply: apply, editor: newInput()}
	f.editor.SetVirtualCursor(true)
	f.editor.DynamicHeight = false
	f.editor.Prompt = ""
	f.editor.CharLimit = 65536
	m.dialog.form = f
	m.focusForm(0)
}
func (m *model) focusForm(index int) {
	f := m.dialog.form
	if f.index < len(f.fields) {
		f.fields[f.index].value = f.editor.Value()
	}
	step := 1
	if index < f.index {
		step = -1
	}
	f.index = (index + len(f.fields) + 2) % (len(f.fields) + 2)
	for !m.settingsFieldVisible(f.index) {
		f.index = (f.index + step + len(f.fields) + 2) % (len(f.fields) + 2)
	}
	f.editor.Blur()
	if f.index < len(f.fields) {
		f.editor.SetValue(f.fields[f.index].value)
		f.editor.Focus()
	}

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
	if f.index < len(f.fields) {
		f.fields[f.index].value = f.editor.Value()
	}
	values := map[string]string{}
	for _, field := range f.fields {
		values[field.key] = field.value
	}
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

	switch msg.String() {
	case "esc":
		m.closeDialog()
		return m, nil
	case "ctrl+s":
		if f.destructive && f.index != len(f.fields) {
			return m, nil
		}
		m.applyForm()
		return m, nil
	case "space":
		if f.index < len(f.fields) && (len(m.formOptions(f.fields[f.index].key)) > 0 || pickerControl(f.fields[f.index].key)) {
			return m, m.openSettingsPicker()
		}
	case "up", "down":
		if f.index >= len(f.fields) || !f.fields[f.index].multiline {
			step := 1
			if msg.String() == "up" {
				step = -1
			}
			m.focusForm(f.index + step)
			return m, nil
		}
	case "ctrl+up":
		m.focusForm(f.index - 1)
		return m, nil
	case "ctrl+down":
		m.focusForm(f.index + 1)
		return m, nil
	case "tab":
		m.focusForm(f.index + 1)
		return m, nil
	case "shift+tab":
		m.focusForm(f.index - 1)
		return m, nil
	case "enter":
		if f.index < len(f.fields) && (len(m.formOptions(f.fields[f.index].key)) > 0 || pickerControl(f.fields[f.index].key)) {
			return m, m.openSettingsPicker()
		}
		if f.index == len(f.fields) {
			m.applyForm()
		} else if f.index == len(f.fields)+1 {
			m.closeDialog()
		} else {
			m.focusForm(f.index + 1)
		}
		return m, nil
	}
	if f.index < len(f.fields) {
		var cmd tea.Cmd
		f.editor, cmd = f.editor.Update(msg)
		return m, cmd
	}
	return m, nil
}
func (m *model) plainFormRows() ([]string, []formHit, int, int) {
	f := m.dialog.form
	w, h := m.vp.Width(), m.vp.Height()
	inner := max(min(w-6, 72), 12)
	height := f.editor.Height()
	rows := []string{m.styles.gate.Render(m.dialog.title), "", m.styles.dim.Render(cellLine(f.hint, inner)), ""}
	capacity := max((h-9-height)/3, 1)
	start := max(min(f.index, len(f.fields)-1)-capacity/2, 0)
	end := min(start+capacity, len(f.fields))
	if end == len(f.fields) {
		start = max(end-capacity, 0)
	}
	hits := []formHit{}
	for i := start; i < end; i++ {
		label := f.fields[i].label
		style := m.styles.dim
		if i == f.index {
			label = "› " + label
			style = m.styles.gate
		}
		y := len(rows)
		rows = append(rows, style.Render(cellLine(label, inner)))
		if i == f.index && len(m.formOptions(f.fields[i].key)) > 0 {
			rows = append(rows, cellLine("[ "+choiceLabel(f.fields[i].key, f.editor.Value())+" ]  Enter to choose", inner))
		} else if i == f.index {
			rows = append(rows, strings.Split(f.editor.View(), "\n")...)
		} else {
			rows = append(rows, cellLine(strings.ReplaceAll(f.fields[i].value, "\n", " ↵ "), inner))
		}
		hits = append(hits, formHit{i, y, len(rows), 0, 0})
		rows = append(rows, "")
	}
	if len(f.fields) > capacity {
		rows = append(rows, m.styles.dim.Render(fmt.Sprintf("Fields %d–%d / %d · Tab moves through all fields", start+1, end, len(f.fields))))
	}
	if f.error != "" {
		rows = append(rows, m.styles.err.Render(cellLine(f.error, inner)))
	} else {
		rows = append(rows, cellLine("", inner))
	}
	for i, label := range []string{f.applyLabel, "Cancel"} {
		style := m.styles.dim
		if f.index == len(f.fields)+i {
			style = m.styles.gate
		}
		y := len(rows)
		rows = append(rows, style.Render(cellLine("[ "+label+" ]", inner)))
		hits = append(hits, formHit{len(f.fields) + i, y, y + 1, 0, 0})
	}
	hint := "↑↓/Tab fields · Ctrl+↑↓ multiline fields · Ctrl+S apply · Esc cancel"
	if f.destructive {
		hint = "Tab selects · Enter confirms selection · Esc cancel"
	}
	rows = append(rows, m.styles.dim.Render(cellLine(hint, inner)))
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
		p.cursor = min(max(p.cursor+step, 0), max(len(p.filtered)-1, 0))
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
		start := max(min(p.cursor-capacity/2, len(p.filtered)-capacity), 0)
		if mouse.X >= x && mouse.X < x+bw+4 && row >= 0 && row < capacity && start+row < len(p.filtered) {
			p.cursor = start + row
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
		if mouse.Y >= y+hit.y0 && mouse.Y < y+hit.y1 {
			if hit.x1 > 0 {
				contentX := 2
				if m.dialog.form.workspace != nil && w >= 76 {
					contentX += 17
				}
				if mouse.X < contentX+hit.x0 || mouse.X >= contentX+hit.x1 {
					continue
				}
			}
			m.focusForm(hit.index)
			if hit.index < len(m.dialog.form.fields) && mouse.Y > y+hit.y0 && (len(m.formOptions(m.dialog.form.fields[hit.index].key)) > 0 || pickerControl(m.dialog.form.fields[hit.index].key)) {
				return m.openSettingsPicker()
			}
			if hit.index == len(m.dialog.form.fields) {
				m.applyForm()
			} else if hit.index == len(m.dialog.form.fields)+1 {
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
	conversation := config.Conversation{}
	if profile.Conversation != nil {
		conversation = *profile.Conversation
	}
	if profile.Conversation != nil {
		profile.Conversation = &conversation
	}
	if profile.Speech != nil {
		copy := *profile.Speech
		profile.Speech = &copy
	}
	fields := []formField{}
	add := func(key, label, value string, multi bool) {
		fields = append(fields, formField{key, label, value, multi})
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
		add("load-profile", "Load a saved profile", "", false)
		add("profile-name", "Save copy as (Ctrl+D)", "custom", false)
	case "character":
		title = "Character and user"
		add("character", "Card path (PNG/JSON; blank removes card)", conversation.Character, false)
		add("user", "Your name", conversation.UserName, false)
	case "generation":
		title = "Generation"
		add("temperature", "Temperature (0 = default 0.4)", strconv.FormatFloat(profile.Generation.Temperature, 'f', -1, 64), false)
		add("max", "Response budget (tokens)", strconv.Itoa(profile.Generation.MaxTokens), false)
		add("context", "Context window (tokens)", strconv.Itoa(profile.Generation.ContextLimit), false)
	case "prompts":
		title = "Conversation prompts"
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
			ws.cache[section] = append([]formField(nil), m.dialog.form.fields...)
			for category, fields := range ws.cache {
				if reflect.DeepEqual(fields, ws.original[category]) {
					continue
				}
				v := map[string]string{}
				for _, field := range fields {
					v[field.key] = field.value
				}
				if category == section {
					v = values
				}
				if category == "application" {
					continue
				}
				if err := applySettingsValues(&profile, category, v); err != nil {
					return fmt.Errorf("%s: %w", category, err)
				}
			}
		} else if err := applySettingsValues(&profile, section, values); err != nil {
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
		if err := m.saveSessionSettings(); err != nil {
			return err
		}
		m.appendBlock(markerBlock("Settings applied to this session"))

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
		m.dialog.form.fields = append([]formField(nil), saved...)
		m.dialog.form.index = len(saved)
		m.focusForm(0)
	}
	m.dialog.title = "Settings"
}

func applySettingsValues(profile *config.Profile, section string, values map[string]string) error {
	conversation := profile.Conversation
	switch section {
	case "character":
		conversation.Character = strings.TrimSpace(values["character"])
		conversation.UserName = strings.TrimSpace(values["user"])
		if conversation.Character != "" {
			path, err := filepath.Abs(conversation.Character)
			if err != nil {
				return err
			}
			conversation.Character = path
		}
	case "generation":
		var err error
		profile.Generation.Temperature, err = strconv.ParseFloat(values["temperature"], 64)
		if err != nil {
			return fmt.Errorf("temperature must be a number")
		}
		profile.Generation.MaxTokens, err = strconv.Atoi(values["max"])
		if err != nil {
			return fmt.Errorf("response budget must be an integer")
		}
		profile.Generation.ContextLimit, err = strconv.Atoi(values["context"])
		if err != nil {
			return fmt.Errorf("context window must be an integer")
		}
	case "prompts":
		conversation.SystemPrompt = values["system"]
		conversation.PostHistory = values["post"]
		main, err := strconv.ParseBool(values["prefer-main"])
		if err != nil {
			return fmt.Errorf("main prompt preference must be true/false")
		}
		post, err := strconv.ParseBool(values["prefer-post"])
		if err != nil {
			return fmt.Errorf("post-history preference must be true/false")
		}
		conversation.PreferCharacterPrompt = &main
		conversation.PreferCharacterPostHistory = &post
	case "speech":
		profile.Speech.Microphone = strings.TrimSpace(values["microphone"])
		variant := strings.TrimSpace(values["variant"])
		model := strings.TrimSpace(values["speech-model"])
		if variant != profile.Speech.Speaking.Variant && filepath.Base(model) == profile.Speech.Speaking.Variant {
			model = filepath.Join(filepath.Dir(model), variant)
		}
		profile.Speech.Speaking.Variant = variant
		path, err := filepath.Abs(model)
		if err != nil {
			return err
		}
		profile.Speech.Speaking.Model = path
		seconds, err := strconv.Atoi(values["seconds"])
		if err != nil {
			return fmt.Errorf("recording seconds must be an integer")
		}
		profile.Speech.MaxSeconds = seconds
		if value, ok := values["vad"]; ok {
			profile.Speech.VAD = value
		}
		if value, ok := values["mic-start"]; ok {
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("speech onset must be milliseconds")
			}
			profile.Speech.MicStartMS = n
		}
		if value, ok := values["mic-end"]; ok {
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("end silence must be milliseconds")
			}
			profile.Speech.MicEndMS = n
		}
		if value, ok := values["mic-threshold"]; ok {
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return fmt.Errorf("threshold must be numeric")
			}
			profile.Speech.MicThreshold = n
		}
		profile.Speech.Enabled = false
	case "model":
		profile.Provider.Ownership = strings.TrimSpace(values["ownership"])
		profile.Provider.Runtime = strings.TrimSpace(values["runtime"])
		profile.Provider.ModelFile = strings.TrimSpace(values["model-file"])
		profile.Provider.ModelRoot = strings.TrimSpace(values["model-root"])
		layers, err := strconv.Atoi(values["gpu-layers"])
		if err != nil {
			return fmt.Errorf("GPU layers must be an integer")
		}
		profile.Provider.GPULayers = layers
		profile.Provider.Kind = strings.TrimSpace(values["kind"])
		profile.Provider.URL = strings.TrimSpace(values["endpoint"])
		profile.Provider.Model = strings.TrimSpace(values["model"])
		profile.Provider.APIKeyEnv = strings.TrimSpace(values["key-env"])
	}
	return nil
}
func (m *model) renameActive() {
	dir, title := filepath.Dir(m.stateFile), m.activeSessionTitle()
	m.openForm("Rename conversation", "Changes the title only; history stays intact.", "Rename", []formField{{"title", "Conversation title", title, false}}, func(values map[string]string) error {
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
	m.openForm("Delete conversation", "Permanently removes this saved conversation.", "Delete conversation", nil, func(map[string]string) error {
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
	m.openForm("Save configuration as profile", "Creates a new file; existing profiles are never overwritten.", "Save profile", []formField{{"path", "New profile file", "profiles/chat-custom.json", false}}, func(values map[string]string) error {
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
	s := m.chat.config.Settings
	m.openDialog("Effective conversation prompt", []string{"Leading system instruction", s.System, "Opening assistant greeting", s.Greeting, "Post-history system directive", s.PostHistory, "This is the compiled prompt; saved conversation turns are inserted between greeting and post-history."})
}

func (m *model) resizeFormEditor() {
	if m.dialog == nil || m.dialog.form == nil {
		return
	}
	f := m.dialog.form
	f.editor.SetWidth(max(min(m.vp.Width()-6, 72), 12))
	height := 1
	if f.index < len(f.fields) && f.fields[f.index].multiline {
		height = min(3, max(m.vp.Height()-10, 1))
	}
	f.editor.SetHeight(height)
}

func (m *model) formOptions(key string) []string {
	if m.dialog != nil && m.dialog.form != nil {
		if choices := m.dialog.form.choices[key]; len(choices) > 0 {
			return choices
		}
	}
	return formChoices(key)
}

func formChoices(key string) []string {
	switch key {
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
	options := m.formOptions(f.fields[f.index].key)
	if len(options) == 0 {
		return
	}
	previous := f.editor.Value()
	next := options[0]
	for i, value := range options {
		if value == previous {
			next = options[(i+1)%len(options)]
			break
		}
	}
	if f.fields[f.index].key == "kind" {
		for i := range f.fields {
			if f.fields[i].key == "endpoint" {
				f.fields[i].value = suggestedBackendURL(f.fields[i].value, previous, next)
			}
		}
	}
	f.editor.SetValue(next)
}
