package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"context"
	"encoding/json"
	"fmt"
	"github.com/keshon/tars/internal/character"
	"github.com/keshon/tars/internal/config"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"time"
)

type settingsWorkspace struct {
	loaded        *config.Profile
	sampleID      uint64
	sampleCancel  context.CancelFunc
	section       string
	nav, advanced bool
	cache         map[string][]formField
	original      map[string][]formField
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
	cursor             int
	loading            bool
	err                string
}
type settingsScanMsg struct {
	id    uint64
	items []string
	err   error
}

var settingsScanID atomic.Uint64
var settingsCategories = []struct{ id, label string }{{"model", "Model"}, {"generation", "Generation"}, {"speech", "Speech"}, {"profiles", "Profiles"}, {"application", "Application"}, {"character", "Character"}, {"prompts", "Prompts"}}

func (m *model) storeSettingsPage() {
	f := m.dialog.form
	if f.index < len(f.fields) {
		f.fields[f.index].value = f.editor.Value()
	}
	f.workspace.cache[f.workspace.section] = append([]formField(nil), f.fields...)
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
	if f.workspace != nil && f.workspace.section == "speech" && index < len(f.fields) {
		switch f.fields[index].key {
		case "vad", "mic-start", "mic-end", "mic-threshold":
			return f.workspace.advanced
		}
	}
	if index >= len(f.fields) || f.workspace == nil || f.workspace.section != "model" {
		return true
	}
	key := f.fields[index].key
	managed := false
	for _, field := range f.fields {
		if field.key == "ownership" {
			v := field.value
			if f.index < len(f.fields) && f.fields[f.index].key == "ownership" {
				v = f.editor.Value()
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
	for i := range m.dialog.form.fields {
		if m.settingsFieldVisible(i) {
			return i
		}
	}
	return len(m.dialog.form.fields)
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
	hint := "Changes apply to this session · history stays intact"
	if f.workspace.section == "application" {
		hint = "Session retained · mode/service changes restart owned services"
	}
	rows := []string{m.styles.gate.Render("Settings / " + category), m.styles.dim.Render(cellLine(hint, inner)), ""}
	hits := []formHit{}
	visible := []int{}
	for i := range f.fields {
		if m.settingsFieldVisible(i) {
			visible = append(visible, i)
		}
	}
	capacity := max((h-10)/3, 1)
	position := 0
	for i, v := range visible {
		if v == f.index {
			position = i
		}
	}
	start := max(min(position-capacity/2, len(visible)-capacity), 0)
	end := min(start+capacity, len(visible))
	for _, i := range visible[start:end] {
		field := f.fields[i]
		label := field.label
		switch field.key {
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
		if i == f.index && !f.workspace.nav {
			style = m.styles.gate
			label = "› " + label
		}
		y := len(rows)
		rows = append(rows, style.Render(cellLine(label, inner)))
		value := field.value
		if i == f.index {
			value = f.editor.Value()
		}
		if pickerFiles(field.key) {
			if value == "" {
				value = "Not selected"
			} else {
				value = filepath.Base(value)
			}
			value += "  [ Choose… ]"
		}
		if field.key == "model" || field.key == "microphone" {
			value = value + "  [ Discover… ]"
		}
		if len(m.formOptions(field.key)) > 0 {
			value = "[ " + choiceLabel(field.key, value) + " ▾ ]"
		}
		if i == f.index && !pickerControl(field.key) && len(m.formOptions(field.key)) == 0 {
			rows = append(rows, strings.Split(f.editor.View(), "\n")...)
		} else {
			rows = append(rows, cellLine(strings.ReplaceAll(value, "\n", " ↵ "), inner))
		}
		hits = append(hits, formHit{i, y, len(rows), 0, 0})
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
			note = "Unsaved changes · session retained · F4 Advanced · F6 Categories"
		}
		if f.workspace.section == "profiles" {
			note = "Ctrl+D saves a copy · Apply uses the loaded profile in this session"
		}
		if f.workspace.section == "speech" {
			note = "Built-in English voice · Ctrl+P plays/stops the applied voice"
		}
	}
	rows = append(rows, m.styles.dim.Render(cellLine(note, inner)))
	buttonRow := ""
	y := len(rows)
	for i, label := range []string{"Apply", "Cancel"} {
		if i > 0 {
			buttonRow += "   "
		}
		start := lipgloss.Width(buttonRow)
		style := m.styles.dim
		if f.index == len(f.fields)+i && (i != 0 || m.settingsDirty()) {
			style = m.styles.gate
		}
		buttonRow += style.Render("[ " + label + " ]")
		hits = append(hits, formHit{len(f.fields) + i, y, y + 1, start, lipgloss.Width(buttonRow)})
	}
	rows = append(rows, cellLine(buttonRow, inner))
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
	if f.index >= len(f.fields) {
		return nil
	}
	key := f.fields[f.index].key
	search := textinput.New()
	search.SetStyles(textInputStyles(search.Styles()))
	search.Prompt = "Search: "
	search.SetVirtualCursor(true)
	search.Focus()
	search.SetWidth(max(m.vp.Width()-14, 12))
	p := &settingsPicker{id: settingsScanID.Add(1), field: key, search: search}
	f.picker = p
	if key == "model" {
		values := map[string]string{}
		for _, field := range f.fields {
			values[field.key] = field.value
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
	selected := f.editor.Value()
	for _, field := range f.fields {
		if field.key == "model-root" {
			root = field.value
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
	p.cursor = min(max(p.cursor, 0), max(len(p.filtered)-1, 0))
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
		p.cursor = max(p.cursor-1, 0)
		return m, nil
	case "down":
		p.cursor = min(p.cursor+1, max(len(p.filtered)-1, 0))
		return m, nil
	case "enter":
		if p.location {
			p.location = false
			p.browser = true
			return m, m.scanPickerDirectory(filepath.Clean(p.search.Value()))
		}
		if p.field != "app-workspace" && p.browser && len(p.filtered) > 0 && strings.HasSuffix(p.filtered[p.cursor], string(os.PathSeparator)) {
			return m, m.scanPickerDirectory(filepath.Clean(p.filtered[p.cursor]))
		}
		if len(p.filtered) > 0 {
			previous := f.editor.Value()
			value := p.filtered[p.cursor]
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
				f.workspace.loaded = &loaded
				f.workspace.cache = map[string][]formField{}
				f.workspace.original = map[string][]formField{}
				f.error = "Profile loaded as a draft; Apply to use it"
			}
			f.editor.SetValue(value)
			f.fields[f.index].value = value
			if p.field == "kind" {
				for i := range f.fields {
					if f.fields[i].key == "endpoint" {
						f.fields[i].value = suggestedBackendURL(f.fields[i].value, previous, value)
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
			for i := range f.fields {
				if f.fields[i].key == "model-root" {
					f.fields[i].value = p.directory
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
	var cmd tea.Cmd
	p.search, cmd = p.search.Update(msg)
	p.cursor = 0
	p.filter()
	return m, cmd
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
		title = "Profile"
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
	start := max(min(p.cursor-capacity/2, len(p.filtered)-capacity), 0)
	end := min(start+capacity, len(p.filtered))
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
		if i == p.cursor {
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
		detail = p.filtered[p.cursor]
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
	endpoint = strings.TrimSuffix(endpoint, "/")
	path := "/v1/models"
	if kind == "kobold" {
		path = "/api/v1/model"
	} else if strings.HasSuffix(endpoint, "/v1") {
		path = "/models"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", endpoint+path, nil)
	if err != nil {
		return nil, err
	}
	if key := os.Getenv(keyEnv); key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("model discovery failed; Esc returns to manual entry")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("model discovery HTTP %d; Esc returns to manual entry", response.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Result string `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("unsupported model list; Esc returns to manual entry")
	}
	items := []string{}
	if kind == "kobold" && payload.Result != "" {
		items = append(items, payload.Result)
	}
	for _, item := range payload.Data {
		if item.ID != "" {
			items = append(items, item.ID)
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no models reported; Esc returns to manual entry")
	}
	return items, nil
}

func (m *model) settingsDimensions() (int, int) {
	if m.dialog != nil && m.dialog.form != nil && m.dialog.form.workspace != nil {
		return max(m.termW-2, 1), max(m.termH-m.actionBarRows()-1, 3)
	}
	return m.vp.Width(), m.vp.Height()
}

func choiceLabel(key, value string) string {
	switch key {
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
		if m.chat == nil && (c.id == "character" || c.id == "prompts") {
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
	if ws.loaded != nil {
		return true
	}
	for category, fields := range ws.cache {
		if category != ws.section && !reflect.DeepEqual(fields, ws.original[category]) {
			return true
		}
	}
	fields := append([]formField(nil), f.fields...)
	if f.index < len(fields) {
		fields[f.index].value = f.editor.Value()
	}
	return !reflect.DeepEqual(fields, ws.original[ws.section])
}
