package settings

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
	"github.com/keshon/tars/internal/speech"
	"github.com/keshon/tars/internal/tui/components/form"
	"github.com/keshon/tars/internal/tui/components/selection"
)

type Picker struct {
	DetailPath, Detail string
	Location           bool
	Browser            bool
	Directory          string
	ID                 uint64
	Field              string
	Items              []string
	Filtered           []string
	Search             textinput.Model
	selection.Model
	Loading bool
	Err     string
}
type ScanMsg struct {
	ID    uint64
	Items []string
	Err   error
}

var scanID atomic.Uint64

func (f *Model) OpenPicker(props Props) tea.Cmd {
	if f.Index >= len(f.Fields) {
		return nil
	}
	key := f.Fields[f.Index].Key
	search := textinput.New()
	search.SetStyles(props.SearchStyles)
	search.Prompt = "Search: "
	search.SetVirtualCursor(true)
	search.Focus()
	search.SetWidth(max(props.Width-14, 12))
	p := &Picker{ID: scanID.Add(1), Field: key, Search: search}
	f.Picker = p
	f.Editor.Blur()
	if key == "model" {
		values := map[string]string{}
		for _, field := range f.Fields {
			values[field.Key] = field.Value
		}
		p.Loading = true
		id := p.ID
		return func() tea.Msg {
			items, err := RemoteModelChoices(values["kind"], values["endpoint"], values["key-env"])
			return ScanMsg{id, items, err}
		}
	}
	if key == "microphone" {
		return f.microphonePicker(p)
	}
	if !PickerFiles(key) {
		p.Items = f.Options(key)
		p.Filter()
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
		p.Browser = true
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
	p.Directory = root
	p.Loading = true
	if key == "app-workspace" {
		return f.ScanDirectory(root)
	}
	id := p.ID
	return func() tea.Msg { items, err := CatalogFiles(root, key); return ScanMsg{id, items, err} }
}

func (p *Picker) Filter() {
	p.Filtered = nil
	query := strings.ToLower(p.Search.Value())
	for _, item := range p.Items {
		if strings.Contains(strings.ToLower(item+" "+ChoiceLabel(p.Field, item)), query) {
			p.Filtered = append(p.Filtered, item)
		}
	}
	p.Cursor = min(max(p.Cursor, 0), max(len(p.Filtered)-1, 0))
}

func (f *Model) AcceptScan(msg ScanMsg) {
	if f == nil || f.Picker == nil {
		return
	}
	p := f.Picker
	if p.ID != msg.ID {
		return
	}
	p.Loading = false
	p.DetailPath = ""
	p.Detail = ""
	p.Items = msg.Items
	if msg.Err != nil {
		p.Err = msg.Err.Error()
	}
	p.Filter()
}

func (f *Model) PickerKey(msg tea.KeyPressMsg, props Props) tea.Cmd {
	defer f.RefreshChoices()
	p := f.Picker
	switch msg.String() {
	case "esc":
		if p.Location {
			p.Location = false
			p.Search.SetValue("")
			p.Filter()
			return nil
		}
		f.Picker = nil
		return nil
	case "up":
		p.Move(-1, len(p.Filtered), 1)
		return nil
	case "down":
		p.Move(1, len(p.Filtered), 1)
		return nil
	case "enter":
		if p.Location {
			p.Location = false
			p.Browser = true
			return f.ScanDirectory(filepath.Clean(p.Search.Value()))
		}
		if p.Field != "app-workspace" && p.Browser && len(p.Filtered) > 0 && strings.HasSuffix(p.Filtered[p.Cursor], string(os.PathSeparator)) {
			return f.ScanDirectory(filepath.Clean(p.Filtered[p.Cursor]))
		}
		if len(p.Filtered) > 0 {
			previous := f.Editor.Value()
			value := p.Filtered[p.Cursor]
			if p.Field == "app-workspace" {
				value = filepath.Clean(value)
			}
			if p.Field == "character" {
				if _, err := character.Load(value); err != nil {
					p.Err = err.Error()
					return nil
				}
			}
			if p.Field == "load-profile" {
				loaded, err := config.Load(value)
				if err != nil {
					p.Err = err.Error()
					return nil
				}
				if f.Chat && loaded.Conversation == nil {
					p.Err = "Choose a chat profile with conversation settings"
					return nil
				}
				f.Workspace.LoadedPath = value
				f.Workspace.Draft.Load(loaded)
				f.Error = "Setup loaded; Apply uses its model, voice and character selection"
			}
			f.Editor.SetValue(value)
			f.Fields[f.Index].Value = value
			if p.Field == "kind" {
				for i := range f.Fields {
					if f.Fields[i].Key == "endpoint" {
						f.Fields[i].Value = SuggestedBackendURL(f.Fields[i].Value, previous, value)
					}
				}
			}
			f.Picker = nil
		}
		return nil
	case "ctrl+r":
		if p.Field == "app-workspace" {
			return f.ScanDirectory(p.Directory)
		}
		if PickerFiles(p.Field) {
			root, key := p.Directory, p.Field
			p.Browser = false
			p.ID = scanID.Add(1)
			p.Loading = true
			p.Search.SetValue("")
			id := p.ID
			return func() tea.Msg { items, err := CatalogFiles(root, key); return ScanMsg{id, items, err} }
		}
		if p.Browser {
			for i := range f.Fields {
				if f.Fields[i].Key == "model-root" {
					f.Fields[i].Value = p.Directory
				}
			}
		}
		return f.OpenPicker(props)
	case "ctrl+b":
		if !PickerFiles(p.Field) {
			return nil
		}
		p.Browser = true
		return f.ScanDirectory(p.Directory)
	case "ctrl+l":
		if !PickerFiles(p.Field) {
			return nil
		}
		p.Location = true
		p.Search.SetValue(p.Directory)
		return nil
	case "ctrl+d":
		if !PickerFiles(p.Field) {
			return nil
		}
		p.Browser = true
		p.ID = scanID.Add(1)
		p.Loading = true
		p.Search.SetValue("")
		id := p.ID
		return func() tea.Msg {
			items := []string{}
			for letter := 'C'; letter <= 'Z'; letter++ {
				root := fmt.Sprintf("%c:%c", letter, os.PathSeparator)
				if info, err := os.Stat(root); err == nil && info.IsDir() {
					items = append(items, root)
				}
			}
			return ScanMsg{id, items, nil}
		}

	}
	return p.UpdateInput(msg)
}

func (p *Picker) UpdateInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	previous := p.Search.Value()
	p.Search, cmd = p.Search.Update(msg)
	if p.Search.Value() != previous {
		p.Cursor = 0
		p.Filter()
	}
	return cmd
}

func (f *Model) PickerRows(props Props) ([]string, []form.Hit, int, int) {
	p := f.Picker
	w, h := props.Width, props.Height
	inner := max(w-6, 12)
	title := p.Field
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
	if p.Location {
		title += " / Location"
	}
	rows := []string{props.Styles.Focus.Render("Choose / " + title), cellLine(p.Search.View(), inner), props.Styles.Muted.Render(strings.Repeat("─", inner))}
	capacity := max(h-10, 1)
	start, end := p.Centered(len(p.Filtered), capacity)
	for i := start; i < end; i++ {
		label := ChoiceLabel(p.Field, p.Filtered[i])
		if PickerFiles(p.Field) {
			if strings.HasSuffix(label, string(os.PathSeparator)) {
				label = "[Dir] " + filepath.Base(filepath.Clean(label))
			} else {
				label = filepath.Base(label)
			}
		}
		style := props.Styles.Muted
		if i == p.Cursor {
			label = "› " + label
			style = props.Styles.Focus
		}
		rows = append(rows, style.Render(cellLine(label, inner)))
	}
	for len(rows) < max(h-6, 3) {
		rows = append(rows, "")
	}
	detail := fmt.Sprintf("%d choices", len(p.Filtered))
	if p.Loading {
		detail = "Discovering available models…"
	} else if p.Err != "" {
		detail = p.Err
	} else if len(p.Filtered) > 0 && PickerFiles(p.Field) {
		detail = p.Filtered[p.Cursor]
		if p.DetailPath == detail && p.Detail != "" {
			detail = p.Detail
		}
	}
	rows = append(rows, props.Styles.Muted.Render(cellLine(detail, inner)), props.Styles.Muted.Render(cellLine("↑↓ choose · Enter select · Esc back · Ctrl+B browse · Ctrl+L location · Ctrl+D drives · Ctrl+R scan", inner)))
	return rows, nil, w, h
}

func (f *Model) ScanDirectory(root string) tea.Cmd {
	p := f.Picker
	p.ID = scanID.Add(1)
	p.Directory = root
	p.Loading = true
	p.Err = ""
	p.Items = nil
	p.Filter()
	p.Search.SetValue("")
	id := p.ID
	key := p.Field
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
			} else if PickerAccepts(key, path) {
				items = append(items, path)
			}
		}
		return ScanMsg{id, items, err}
	}
}

func (f *Model) microphonePicker(p *Picker) tea.Cmd {
	cfg := f.Workspace.Draft.Base()
	id := p.ID
	p.Loading = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		items, err := speech.Microphones(ctx, cfg.Speech.Recorder)
		return ScanMsg{id, items, err}
	}
}

func (f *Model) DetailCommand() tea.Cmd {
	if f == nil || f.Picker == nil {
		return nil
	}
	p := f.Picker
	if !PickerFiles(p.Field) || len(p.Filtered) == 0 || p.Loading {
		return nil
	}
	path := p.Filtered[p.Cursor]
	if path == p.DetailPath {
		return nil
	}
	p.DetailPath = path
	p.Detail = ""
	id, key := p.ID, p.Field
	return func() tea.Msg {
		text := CatalogDetail(key, path)
		if key == "model-file" {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				text = fmt.Sprintf("%.2f GiB · %s", float64(info.Size())/(1<<30), text)
			}
		}
		return DetailMsg{id, path, text}
	}
}

type DetailMsg struct {
	ID         uint64
	Path, Text string
}

func (f *Model) AcceptDetail(msg DetailMsg) {
	if f == nil || f.Picker == nil {
		return
	}
	p := f.Picker
	if p.ID == msg.ID && p.DetailPath == msg.Path {
		p.Detail = msg.Text
	}
}
