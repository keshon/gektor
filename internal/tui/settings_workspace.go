package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/tars/internal/character"
	"github.com/keshon/tars/internal/config"
	"github.com/keshon/tars/internal/llm"
	"github.com/keshon/tars/internal/prompts"
	"github.com/keshon/tars/internal/tui/components/selection"
	"github.com/keshon/tars/internal/tui/settings"
)

type settingsWorkspace struct {
	draft         *settings.Draft
	sampleID      uint64
	sampleCancel  context.CancelFunc
	section       string
	loadedPath    string
	autoSaved     bool
	nav, advanced bool
}
type settingsPicker struct {
	detailPath, detail string
	location           bool
	browser            bool
	directory          string
	id                 uint64
	field              string
	items              []string
	filtered           []string
	search             textinput.Model
	selection.Model
	loading bool
	err     string
}
type settingsScanMsg struct {
	id    uint64
	items []string
	err   error
}

var settingsScanID atomic.Uint64
var settingsCategories = []struct{ id, label string }{{"model", "Model"}, {"generation", "Generation"}, {"speech", "Speech"}, {"profiles", "Profiles"}, {"application", "Application"}, {"character", "Character"}, {"you", "You"}, {"prompts", "Prompts"}}

func (m *model) storeSettingsPage() {
	f := m.dialog.form
	if f.Index < len(f.Fields) {
		f.Fields[f.Index].Value = f.Editor.Value()
	}
	f.workspace.draft.Store(f.workspace.section, f.Fields)
}
func (m *model) settingsNavKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	categories := m.availableSettingsCategories()
	f := m.dialog.form
	ws := f.workspace
	if msg.String() == "ctrl+s" {
		m.applyForm()
		return m, nil
	}
	if msg.String() == "esc" {
		ws.nav = false
		return m, nil
	}
	if msg.String() == "enter" || msg.String() == "right" || msg.String() == "tab" {
		ws.nav = false
		return m, nil
	}
	if msg.String() == "up" || msg.String() == "down" {
		index := 0
		for i, c := range categories {
			if c.id == ws.section {
				index = i
			}
		}
		step := 1
		if msg.String() == "up" {
			step = -1
		}
		for n := 0; n < len(categories); n++ {
			index = (index + step + len(categories)) % len(categories)
			c := categories[index]
			if c.id != "speech" || m.hasSettingsSpeech() {
				if c.id != ws.section {
					m.editSettings(c.id)
				}
				m.dialog.form.workspace.nav = true
				break
			}
		}
	}
	return m, nil
}
func (m *model) settingsFieldVisible(index int) bool {
	f := m.dialog.form
	if index < len(f.Fields) && f.workspace != nil && f.workspace.section == "character" && f.Fields[index].Key == "name" {
		for _, field := range f.Fields {
			if field.Key == "character" && strings.TrimSpace(field.Value) != "" {
				return false
			}
		}
	}
	if f.workspace != nil && f.workspace.section == "speech" && index < len(f.Fields) {
		switch f.Fields[index].Key {
		case "vad", "mic-start", "mic-end", "mic-threshold":
			return f.workspace.advanced
		}
	}
	if index >= len(f.Fields) || f.workspace == nil || f.workspace.section != "model" {
		return true
	}
	key := f.Fields[index].Key
	managed := false
	for _, field := range f.Fields {
		if field.Key == "ownership" {
			v := field.Value
			if f.Index < len(f.Fields) && f.Fields[f.Index].Key == "ownership" {
				v = f.Editor.Value()
			}
			managed = v == "managed"
		}
	}
	if key == "ownership" {
		return true
	}
	if managed {
		switch key {
		case "model-file", "gpu-layers":
			return true
		case "runtime", "model-root", "endpoint":
			return f.workspace.advanced
		default:
			return false
		}
	}
	switch key {
	case "runtime", "model-root", "model-file", "gpu-layers":
		return false
	case "key-env":
		return f.workspace.advanced
	default:
		return true
	}
}
func (m *model) firstVisibleField() int {
	for i := range m.dialog.form.Fields {
		if m.settingsFieldVisible(i) {
			return i
		}
	}
	return len(m.dialog.form.Fields)
}
func (m *model) formRows() ([]string, []formHit, int, int) {
	f := m.dialog.form
	if f.picker != nil {
		return m.pickerRows()
	}
	if f.workspace == nil {
		return m.plainFormRows()
	}
	w, h := m.settingsDimensions()
	rail := 0
	if w >= 76 {
		rail = 17
	}
	inner := max(w-6-rail, 12)
	category := ""
	if f.workspace.section == "application" {
		category = "Application"
	}
	for _, c := range settingsCategories {
		if c.id == f.workspace.section {
			category = c.label
		}
	}
	hint := settingsHint(f.workspace.section)
	rows := []string{m.styles.gate.Render("Settings / " + category), m.styles.dim.Render(cellLine(hint, inner)), ""}
	hits := []formHit{}
	visible := []int{}
	for i := range f.Fields {
		if m.settingsFieldVisible(i) {
			visible = append(visible, i)
		}
	}
	capacity := max((h-10)/3, 1)
	position := 0
	for i, v := range visible {
		if v == f.Index {
			position = i
		}
	}
	start := max(min(position-capacity/2, len(visible)-capacity), 0)
	end := min(start+capacity, len(visible))
	for _, i := range visible[start:end] {
		field := f.Fields[i]
		label := field.Label
		switch field.Key {
		case "ownership":
			label = "Connection"
		case "model-file":
			label = "Model"
		case "gpu-layers":
			label = "GPU layers"
		case "endpoint":
			label = "Server address"
		case "kind":
			label = "Protocol"
		}
		style := m.styles.dim
		if i == f.Index && !f.workspace.nav {
			style = m.styles.gate
			label = "› " + label
		}
		y := len(rows)
		rows = append(rows, style.Render(cellLine(label, inner)))
		value := field.Value
		if i == f.Index {
			value = f.Editor.Value()
		}
		if pickerFiles(field.Key) {
			if value == "" {
				value = "Not selected"
			} else {
				value = filepath.Base(value)
			}
			value += "  [ Choose… ]"
		}
		if field.Key == "model" || field.Key == "microphone" {
			value = value + "  [ Discover… ]"
		}
		if len(m.formOptions(field.Key)) > 0 {
			value = "[ " + choiceLabel(field.Key, value) + " ▾ ]"
		}
		if i == f.Index && !pickerControl(field.Key) && len(m.formOptions(field.Key)) == 0 {
			rows = append(rows, strings.Split(f.Editor.View(), "\n")...)
		} else {
			rows = append(rows, cellLine(strings.ReplaceAll(value, "\n", " ↵ "), inner))
		}
		hits = append(hits, formHit{Index: i, Y0: y, Y1: len(rows), X0: 0, X1: 0})
		rows = append(rows, "")
	}
	// Actions stay at the bottom; a small terminal shows fewer controls.
	for len(rows) < max(h-7, 3) {
		rows = append(rows, "")
	}
	note := f.error
	if note == "" {
		note = "No changes · F4 Advanced · F6 Categories"
		if m.settingsDirty() {
			note = "Apply saves a custom profile · F4 Advanced · F6 Categories"
		}
		if f.workspace.section == "profiles" {
			note = "Ctrl+D saves a copy · Apply uses the loaded profile in this session"
		}
		if f.workspace.section == "speech" {
			note = "Built-in English voice · Ctrl+P plays/stops the applied voice"
		}
	}
	rows = append(rows, m.styles.dim.Render(cellLine(note, inner)))
	buttonRow, actionHits := m.formActions(inner, len(rows), "Apply", m.settingsDirty())
	hits = append(hits, actionHits...)
	rows = append(rows, buttonRow, m.styles.dim.Render(strings.Repeat("─", inner)))
	rows = append(rows, m.styles.dim.Render(cellLine("Tab controls · Ctrl+S apply · Esc cancel", inner)))
	if rail > 0 {
		for i := range rows {
			label := ""
			if categories := m.availableSettingsCategories(); i < len(categories) {
				c := categories[i]
				if c.id != "speech" || m.hasSettingsSpeech() {
					label = "  " + c.label
					if c.id == f.workspace.section {
						label = "› " + c.label
						style := m.styles.hunk
						if f.workspace.nav {
							style = m.styles.gate
						}
						label = style.Render(label)
					}
				}
			}
			rows[i] = cellLine(label, rail-2) + m.styles.dim.Render("│ ") + cellLine(rows[i], inner)
		}
	}
	return rows, hits, w, h
}
func (m *model) openSettingsPicker() tea.Cmd {
	f := m.dialog.form
	if f.Index >= len(f.Fields) {
		return nil
	}
	key := f.Fields[f.Index].Key
	search := textinput.New()
	search.SetStyles(textInputStyles(search.Styles()))
	search.Prompt = "Search: "
	search.SetVirtualCursor(true)
	search.Focus()
	search.SetWidth(max(m.transcript.Viewport.Width()-14, 12))
	p := &settingsPicker{id: settingsScanID.Add(1), field: key, search: search}
	f.picker = p
	f.Editor.Blur()
	if key == "model" {
		values := map[string]string{}
		for _, field := range f.Fields {
			values[field.Key] = field.Value
		}
		p.loading = true
		id := p.id
		return func() tea.Msg {
			items, err := remoteModelChoices(values["kind"], values["endpoint"], values["key-env"])
			return settingsScanMsg{id, items, err}
		}
	}
	if key == "microphone" {
		return m.microphonePicker(p)
	}
	if !pickerFiles(key) {
		p.items = m.formOptions(key)
		p.filter()
		return nil
	}
	root := ""
	selected := f.Editor.Value()
	for _, field := range f.Fields {
		if field.Key == "model-root" {
			root = field.Value
		}
	}
	if root == "" {
		root = filepath.Dir(selected)
	}
	if key == "app-workspace" {
		root = selected
		if root == "" {
			root, _ = os.Getwd()
		}
		p.browser = true
	}
	if root == "." || root == "" {
		exe, _ := os.Executable()
		root = filepath.Join(filepath.Dir(exe), "assets", "models", "thinking")
		if key == "character" {
			root = filepath.Join(filepath.Dir(exe), "assets", "characters")
		}
		if key == "load-profile" || key == "app-profile" {
			root = filepath.Join(filepath.Dir(exe), "profiles")
		}
	}
	if key == "load-profile" {
		exe, _ := os.Executable()
		root = filepath.Join(filepath.Dir(exe), "profiles")
	}
	p.directory = root
	p.loading = true
	if key == "app-workspace" {
		return m.scanPickerDirectory(root)
	}
	id := p.id
	return func() tea.Msg { items, err := catalogFiles(root, key); return settingsScanMsg{id, items, err} }
}
func (p *settingsPicker) filter() {
	p.filtered = nil
	query := strings.ToLower(p.search.Value())
	for _, item := range p.items {
		if strings.Contains(strings.ToLower(item+" "+choiceLabel(p.field, item)), query) {
			p.filtered = append(p.filtered, item)
		}
	}
	p.Cursor = min(max(p.Cursor, 0), max(len(p.filtered)-1, 0))
}
func (m *model) acceptSettingsScan(msg settingsScanMsg) {
	if m.dialog == nil || m.dialog.form == nil || m.dialog.form.picker == nil {
		return
	}
	p := m.dialog.form.picker
	if p.id != msg.id {
		return
	}
	p.loading = false
	p.detailPath = ""
	p.detail = ""
	p.items = msg.items
	if msg.err != nil {
		p.err = msg.err.Error()
	}
	p.filter()
}
func (m *model) pickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := m.dialog.form
	p := f.picker
	switch msg.String() {
	case "esc":
		if p.location {
			p.location = false
			p.search.SetValue("")
			p.filter()
			return m, nil
		}
		f.picker = nil
		return m, nil
	case "up":
		p.Move(-1, len(p.filtered), 1)
		return m, nil
	case "down":
		p.Move(1, len(p.filtered), 1)
		return m, nil
	case "enter":
		if p.location {
			p.location = false
			p.browser = true
			return m, m.scanPickerDirectory(filepath.Clean(p.search.Value()))
		}
		if p.field != "app-workspace" && p.browser && len(p.filtered) > 0 && strings.HasSuffix(p.filtered[p.Cursor], string(os.PathSeparator)) {
			return m, m.scanPickerDirectory(filepath.Clean(p.filtered[p.Cursor]))
		}
		if len(p.filtered) > 0 {
			previous := f.Editor.Value()
			value := p.filtered[p.Cursor]
			if p.field == "app-workspace" {
				value = filepath.Clean(value)
			}
			if p.field == "character" {
				if _, err := character.Load(value); err != nil {
					p.err = err.Error()
					return m, nil
				}
			}
			if p.field == "load-profile" {
				loaded, err := config.Load(value)
				if err != nil {
					p.err = err.Error()
					return m, nil
				}
				if m.chat != nil && loaded.Conversation == nil {
					p.err = "Choose a chat profile with conversation settings"
					return m, nil
				}
				f.workspace.loadedPath = value
				f.workspace.draft.Load(loaded)
				f.error = "Setup loaded; Apply uses its model, voice and character selection"
			}
			f.Editor.SetValue(value)
			f.Fields[f.Index].Value = value
			if p.field == "kind" {
				for i := range f.Fields {
					if f.Fields[i].Key == "endpoint" {
						f.Fields[i].Value = suggestedBackendURL(f.Fields[i].Value, previous, value)
					}
				}
			}
			f.picker = nil
		}
		return m, nil
	case "ctrl+r":
		if p.field == "app-workspace" {
			return m, m.scanPickerDirectory(p.directory)
		}
		if pickerFiles(p.field) {
			root, key := p.directory, p.field
			p.browser = false
			p.id = settingsScanID.Add(1)
			p.loading = true
			p.search.SetValue("")
			id := p.id
			return m, func() tea.Msg { items, err := catalogFiles(root, key); return settingsScanMsg{id, items, err} }
		}
		if p.browser {
			for i := range f.Fields {
				if f.Fields[i].Key == "model-root" {
					f.Fields[i].Value = p.directory
				}
			}
		}
		return m, m.openSettingsPicker()
	case "ctrl+b":
		if !pickerFiles(p.field) {
			return m, nil
		}
		p.browser = true
		return m, m.scanPickerDirectory(p.directory)
	case "ctrl+l":
		if !pickerFiles(p.field) {
			return m, nil
		}
		p.location = true
		p.search.SetValue(p.directory)
		return m, nil
	case "ctrl+d":
		if !pickerFiles(p.field) {
			return m, nil
		}
		p.browser = true
		p.id = settingsScanID.Add(1)
		p.loading = true
		p.search.SetValue("")
		id := p.id
		return m, func() tea.Msg {
			items := []string{}
			for letter := 'C'; letter <= 'Z'; letter++ {
				root := fmt.Sprintf("%c:%c", letter, os.PathSeparator)
				if info, err := os.Stat(root); err == nil && info.IsDir() {
					items = append(items, root)
				}
			}
			return settingsScanMsg{id, items, nil}
		}

	}
	return m, p.updateInput(msg)
}

func (p *settingsPicker) updateInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	previous := p.search.Value()
	p.search, cmd = p.search.Update(msg)
	if p.search.Value() != previous {
		p.Cursor = 0
		p.filter()
	}
	return cmd
}
func (m *model) pickerRows() ([]string, []formHit, int, int) {
	p := m.dialog.form.picker
	w, h := m.settingsDimensions()
	inner := max(w-6, 12)
	title := p.field
	switch title {
	case "character":
		title = "Character card"
	case "model-file":
		title = "Thinking model"
	case "load-profile", "app-profile":
		title = "Profile / saved setup"
	case "app-workspace":
		title = "Agent workspace"
	case "microphone":
		title = "Microphone"
	}
	if p.location {
		title += " / Location"
	}
	rows := []string{m.styles.gate.Render("Choose / " + title), cellLine(p.search.View(), inner), m.styles.dim.Render(strings.Repeat("─", inner))}
	capacity := max(h-10, 1)
	start, end := p.Centered(len(p.filtered), capacity)
	for i := start; i < end; i++ {
		label := choiceLabel(p.field, p.filtered[i])
		if pickerFiles(p.field) {
			if strings.HasSuffix(label, string(os.PathSeparator)) {
				label = "[Dir] " + filepath.Base(filepath.Clean(label))
			} else {
				label = filepath.Base(label)
			}
		}
		style := m.styles.dim
		if i == p.Cursor {
			label = "› " + label
			style = m.styles.gate
		}
		rows = append(rows, style.Render(cellLine(label, inner)))
	}
	for len(rows) < max(h-6, 3) {
		rows = append(rows, "")
	}
	detail := fmt.Sprintf("%d choices", len(p.filtered))
	if p.loading {
		detail = "Discovering available models…"
	} else if p.err != "" {
		detail = p.err
	} else if len(p.filtered) > 0 && pickerFiles(p.field) {
		detail = p.filtered[p.Cursor]
		if p.detailPath == detail && p.detail != "" {
			detail = p.detail
		}
	}
	rows = append(rows, m.styles.dim.Render(cellLine(detail, inner)), m.styles.dim.Render(cellLine("↑↓ choose · Enter select · Esc back · Ctrl+B browse · Ctrl+L location · Ctrl+D drives · Ctrl+R scan", inner)))
	return rows, nil, w, h
}

func (m *model) scanPickerDirectory(root string) tea.Cmd {
	p := m.dialog.form.picker
	p.id = settingsScanID.Add(1)
	p.directory = root
	p.loading = true
	p.err = ""
	p.items = nil
	p.filter()
	p.search.SetValue("")
	id := p.id
	key := p.field
	return func() tea.Msg {
		entries, err := os.ReadDir(root)
		items := []string{}
		parent := filepath.Dir(root)
		if parent != root {
			items = append(items, parent+string(os.PathSeparator))
		}
		for _, entry := range entries {
			path := filepath.Join(root, entry.Name())
			if entry.IsDir() {
				items = append(items, path+string(os.PathSeparator))
			} else if pickerAccepts(key, path) {
				items = append(items, path)
			}
		}
		return settingsScanMsg{id, items, err}
	}
}

func remoteModelChoices(kind, endpoint, keyEnv string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	items, err := llm.DiscoverModels(ctx, kind, endpoint, os.Getenv(keyEnv))
	if err != nil {
		return nil, fmt.Errorf("%w; Esc returns to manual entry", err)
	}
	return items, nil
}

func (m *model) settingsDimensions() (int, int) {
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.workspace != nil {
		return max(m.termW-2, 1), max(m.termH-m.actionBarRows()-1, 3)
	}
	return m.transcript.Viewport.Width(), m.transcript.Viewport.Height()
}

func choiceLabel(key, value string) string {
	switch key {
	case "prompt-preset":
		preset, err := prompts.ResponsePreset(value)
		if err == nil {
			return preset.Name
		}
	case "greeting":
		if value == "0" {
			return "Main greeting"
		}
		return "Alternate greeting " + value
	case "theme":
		if value == "light" {
			return "Light"
		}
		return "Dark"
	case "app-mode":
		if value == "chat" {
			return "Chat · no tools"
		}
		if value == "agent" {
			return "Agent · workspace tools"
		}
	case "app-voice":
		switch value {
		case "off":
			return "Off · text only"
		case "ptt":
			return "Push-to-talk"
		case "live":
			return "Hands-free"
		}
	case "ownership":
		if value == "managed" {
			return "Local runtime · llama.cpp"
		}
		if value == "external" {
			return "Connect to a running server"
		}
	case "variant":
		if value == "nano" {
			return "Chatterbox Nano"
		}
		if value == "turbo" {
			return "Chatterbox Turbo"
		}
	case "prefer-main", "prefer-post":
		if value == "true" {
			return "Enabled"
		}
		if value == "false" {
			return "Disabled"
		}
	}
	return value
}

func (m *model) availableSettingsCategories() []struct{ id, label string } {
	out := []struct{ id, label string }{}
	for _, c := range settingsCategories {
		if c.id == "application" && m.application == nil {
			continue
		}
		if c.id == "speech" && !m.hasSettingsSpeech() {
			continue
		}
		out = append(out, c)
	}

	return out
}

func (m *model) settingsDirty() bool {
	if m.dialog == nil || m.dialog.form == nil || m.dialog.form.workspace == nil {
		return true
	}
	f := m.dialog.form
	ws := f.workspace
	fields := append([]formField(nil), f.Fields...)
	if f.Index < len(fields) {
		fields[f.Index].Value = f.Editor.Value()
	}
	return ws.draft.Dirty(ws.section, fields)
}

func settingsHint(section string) string {
	switch section {
	case "profiles":
		return "Profiles save your setup, including the selected character."
	case "character":
		return "Character sets identity and dialogue in both Chat and Agent."
	case "you":
		return "Your name and persona are used by the character's prompts."
	case "application":
		return "Mode sets capabilities; switching keeps the selected character."
	case "model":
		return "Choose the thinking model; the character stays selected."
	case "speech":
		return "Choose voice services; the character stays selected."
	case "prompts":
		return "Preset adds response style; card overrides affect base instructions."
	}
	return "Changes apply to this session; history stays intact."
}
