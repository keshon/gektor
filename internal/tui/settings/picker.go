package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/selection"
)

type Picker struct {
	Parent             string
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
	if key == "reasoning-effort" {
		p.Loading = true
		id, discover := p.ID, f.DiscoverReasoning
		profile := f.Workspace.Draft.Base()
		if fields, ok := f.Workspace.Draft.Fields("model"); ok {
			values := map[string]string{}
			for _, field := range fields {
				values[field.Key] = field.Value
			}
			if err := config.ApplySection(&profile, "model", values); err != nil {
				p.Loading = false
				p.Err = err.Error()
				return nil
			}
		}
		return func() tea.Msg {
			if discover == nil {
				return ScanMsg{ID: id, Err: fmt.Errorf("reasoning options unavailable")}
			}
			options, err := discover(profile.Provider.Kind, profile.Provider.URL, profile.Provider.Model)
			if err != nil {
				return ScanMsg{ID: id, Err: err}
			}
			return ScanMsg{ID: id, Items: append([]string{"default"}, options.Efforts...)}
		}
	}
	if key == "model" {
		values := map[string]string{}
		for _, field := range f.Fields {
			values[field.Key] = field.Value
		}
		p.Loading = true
		id := p.ID
		discover := f.DiscoverModels
		return func() tea.Msg {
			if discover == nil {
				return ScanMsg{ID: id, Err: fmt.Errorf("model discovery unavailable; enter model manually")}
			}
			items, err := discover(values["kind"], values["endpoint"], values["key-env"])
			return ScanMsg{id, items, err}
		}
	}
	if !PickerFiles(key) {
		p.Items = f.Options(key)
		p.Filter()
		for i, item := range p.Filtered {
			if item == f.Editor.Value() {
				p.Cursor = i
				break
			}
		}
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
		if key == "load-profile" {
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
		if p.Browser && len(p.Filtered) > 0 && strings.HasSuffix(p.Filtered[p.Cursor], string(os.PathSeparator)) {
			return f.ScanDirectory(filepath.Clean(p.Filtered[p.Cursor]))
		}
		if len(p.Filtered) > 0 {
			previous := f.Editor.Value()
			value := p.Filtered[p.Cursor]
			if p.Field == "app-workspace" {
				value = filepath.Clean(value)
			}
			if p.Field == "load-profile" {
				loaded, err := config.Load(value)
				if err != nil {
					p.Err = err.Error()
					return nil
				}
				f.Workspace.LoadedPath = value
				f.Workspace.Draft.Load(loaded)
				f.Error = "Setup loaded; Apply uses its model and identity selection"
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
	case "ctrl+s":
		if p.Field == "app-workspace" && !p.Loading && p.Err == "" && p.Directory != "" {
			f.Editor.SetValue(p.Directory)
			f.Fields[f.Index].Value = p.Directory
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
		p.Directory, p.Parent = "", ""
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
	if p.Field == "theme" {
		return nil
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

func (p *Picker) listTop() int {
	if p.Field == "theme" {
		return 2
	}
	if PickerFiles(p.Field) {
		return 4
	}
	return 3
}
func (p *Picker) capacity(height int) int { return max(height-7-p.listTop(), 1) }

func (f *Model) modalWidth(props Props) int {
	if p := f.Picker; p != nil && !PickerFiles(p.Field) && len(p.Items) <= 12 {
		return min(props.Width, 52)
	}
	return props.Width
}

func (f *Model) PickerRows(props Props) ([]string, []form.Hit, int, int) {
	p := f.Picker
	w, h := f.modalWidth(props), props.Height
	inner := max(popup.ModalWidth(w)-4, 1)
	title := p.Field
	if title == "load-profile" {
		title = "Profile / saved setup"
	}
	if title == "app-workspace" {
		title = "Agent workspace"
	}
	if p.Location {
		title += " / Location"
	}
	rows := []string{popup.TitleRow(props.Styles.Focus.Render("Choose / "+title), inner), props.Styles.Muted.Render(strings.Repeat("─", inner))}
	if PickerFiles(p.Field) {
		rows = append(rows, cell.Line(p.Directory, inner))
	}
	if p.Field != "theme" {
		p.Search.SetWidth(max(inner-8, 1))
		rows = append(rows, cell.Line(p.Search.View(), inner))
	}
	capacity := p.capacity(h)
	start, end := p.Centered(len(p.Filtered), capacity)
	body := []string{}
	for i := start; i < end; i++ {
		label := ChoiceLabel(p.Field, p.Filtered[i])
		if PickerFiles(p.Field) {
			if p.Browser && filepath.Clean(p.Filtered[i]) == p.Parent && p.Parent != p.Directory {
				label = "..."
			} else if strings.HasSuffix(label, string(os.PathSeparator)) {
				label = "[Dir] " + filepath.Base(filepath.Clean(label))
			} else {
				label = filepath.Base(label)
			}
		}
		style := props.Styles.Muted
		prefix := "  "
		if i == p.Cursor {
			prefix = "› "
			style = props.Styles.Focus
		}
		body = append(body, style.Render(cell.Line(prefix+label, max(inner-1, 1))))
	}
	rows = append(rows, popup.ScrollRows(body, inner, start, len(p.Filtered), props.Surface.Normal)...)
	detail := fmt.Sprintf("%d choices", len(p.Filtered))
	if p.Loading {
		detail = "Loading…"
	} else if p.Err != "" {
		detail = p.Err
	} else if len(p.Filtered) > 0 && PickerFiles(p.Field) {
		detail = p.Filtered[p.Cursor]
		if p.DetailPath == detail && p.Detail != "" {
			detail = p.Detail
		}
	}
	rows = append(rows, props.Styles.Muted.Render(cell.Line(detail, inner)))
	hits := []form.Hit{{Index: -3, Y0: 0, Y1: 1, X0: max(inner-3, 0), X1: inner}}
	if p.Field == "app-workspace" {
		label := "[ Use this folder ]  Ctrl+S"
		hits = append(hits, form.Hit{Index: -2, Y0: len(rows), Y1: len(rows) + 1, X0: 0, X1: len("[ Use this folder ]")})
		rows = append(rows, props.Styles.Focus.Render(cell.Line(label, inner)))
	}
	hint := "↑↓ choose · Enter select · Esc back"
	if PickerFiles(p.Field) {
		hint = "Enter open · Esc back · Ctrl+L path · Ctrl+D drives"
	}
	rows = append(rows, props.Styles.Muted.Render(cell.Line(hint, inner)))
	return rows, hits, w, h
}

func (f *Model) ScanDirectory(root string) tea.Cmd {
	p := f.Picker
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	root = filepath.Clean(root)
	p.Parent = filepath.Dir(root)
	p.ID = scanID.Add(1)
	p.Directory = root
	p.Cursor = 0
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
