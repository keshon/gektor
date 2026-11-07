package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/selection"
)

func (p *Picker) IsFiles() bool { return p.FileConfig != nil || PickerFiles(p.Field) }

type Picker struct {
	// Control is 0 for search/results, 1 for Select/Open, 2 for Cancel.
	Control            int
	FileConfig         *FilePicker
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
	if config, ok := f.FileFields[key]; ok {
		p.FileConfig = &config
		p.Browser = true
		return f.ScanDirectory(config.Root)
	}
	if !f.FilePicker(key) {
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
	key := msg.String()
	if key == "tab" || key == "shift+tab" {
		step := 1
		if key == "shift+tab" {
			step = -1
		}
		p.Control = (p.Control + step + 3) % 3
		return nil
	}
	if p.Control > 0 && (key == "left" || key == "right") {
		p.Control = 3 - p.Control
		return nil
	}
	if p.Control > 0 && (key == "enter" || key == "space") {
		if p.Control == 2 {
			f.Picker = nil
			return nil
		}
		if p.Field == "app-workspace" {
			return f.PickerKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, props)
		}
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	if key == "up" || key == "down" || key == "pgup" || key == "pgdown" {
		p.Control = 0
	}
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
	case "pgup":
		p.Move(-p.capacity(props.Height), len(p.Filtered), p.capacity(props.Height))
		return nil
	case "pgdown":
		p.Move(p.capacity(props.Height), len(p.Filtered), p.capacity(props.Height))
		return nil
	case "home", "ctrl+home":
		if p.Location || (msg.String() == "home" && p.Field != "theme") {
			return p.UpdateInput(msg)
		}
		p.Cursor = 0
		return nil
	case "end", "ctrl+end":
		if p.Location || (msg.String() == "end" && p.Field != "theme") {
			return p.UpdateInput(msg)
		}
		p.Cursor = max(len(p.Filtered)-1, 0)
		return nil
	case "enter":
		if p.Loading {
			return nil
		}
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
		if p.Field == "app-workspace" || p.FileConfig != nil {
			return f.ScanDirectory(p.Directory)
		}
		if p.IsFiles() {
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
		if !p.IsFiles() {
			return nil
		}
		p.Browser = true
		return f.ScanDirectory(p.Directory)
	case "ctrl+l":
		if !p.IsFiles() {
			return nil
		}
		p.Location = true
		p.Search.SetValue(p.Directory)
		return nil
	case "ctrl+d":
		if !p.IsFiles() {
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
	if p.Field == "theme" || p.Control > 0 {
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
	if p.Field == "app-workspace" {
		return 6
	}
	if p.Field == "theme" {
		return 2
	}
	if p.IsFiles() {
		return 4
	}
	return 3
}
func (p *Picker) capacity(height int) int {
	if p.Field == "app-workspace" {
		return max(height-12-p.listTop(), 1)
	}
	return max(height-9-p.listTop(), 1)
}

func (f *Model) PickerRows(props Props) ([]string, []form.Hit, int, int) {
	p := f.Picker
	w, h := props.Width, props.Height
	inner := max(popup.ModalWidth(w)-4, 1)
	title := p.Field
	for _, field := range f.Fields {
		if field.Key == p.Field && field.Label != "" {
			title = field.Label
			break
		}
	}
	if title == "load-profile" {
		title = "Profile / saved setup"
	}
	if title == "app-workspace" {
		title = "Workspace"
	}
	if p.Location {
		title += " / Location"
	}
	rows := popup.Header("Select "+title, inner, props.Styles.Title)
	if p.IsFiles() {
		location := p.Directory
		if p.Field == "app-workspace" {
			available := max(inner-cell.Width("Current folder: "), 1)
			if extra := cell.Width(location) - available; extra > 0 {
				location = ansi.TruncateLeft(location, extra+1, "…")
			}
			location = "Current folder: " + location
		}
		rows = append(rows, cell.Line(location, inner))
	}
	if p.Field == "app-workspace" {
		rows = append(rows, "")
		p.Search.Prompt = "Filter folders: "
		if p.Location {
			p.Search.Prompt = "Folder path: "
		}
	}
	if p.Field != "theme" {
		p.Search.SetWidth(max(inner-cell.Width(p.Search.Prompt)-2, 1))
		rows = append(rows, cell.Line(p.Search.View(), inner))
	}
	if p.Field == "app-workspace" {
		rows = append(rows, "")
	}
	capacity := p.capacity(h)
	start, end := p.Centered(len(p.Filtered), capacity)
	body := []string{}
	hits := []form.Hit{{Index: -3, Y0: 0, Y1: 1, X0: max(inner-3, 0), X1: inner}}
	for i := start; i < end; i++ {
		label := ChoiceLabel(p.Field, p.Filtered[i])
		if p.IsFiles() {
			if p.Browser && filepath.Clean(p.Filtered[i]) == p.Parent && p.Parent != p.Directory {
				label = "Parent folder"
			} else if strings.HasSuffix(label, string(os.PathSeparator)) {
				label = "[Dir] " + filepath.Base(filepath.Clean(label))
			} else {
				label = filepath.Base(label)
			}
		}
		style := props.Surface.Normal
		prefix := "  "
		if i == p.Cursor {
			prefix = "› "
			style = props.Surface.Selected
		}
		hits = append(hits, form.Hit{Index: -5, Option: i + 1, Y0: len(rows) + len(body), Y1: len(rows) + len(body) + 1, X1: max(inner-2, 1)})
		body = append(body, style.Render(cell.Line(prefix+label, max(inner-1, 1))))
	}
	rows = append(rows, popup.ScrollRows(body, inner, start, len(p.Filtered), props.Surface.Normal)...)
	detail := fmt.Sprintf("%d choices", len(p.Filtered))
	if p.Loading {
		detail = "Loading…"
	} else if p.Err != "" {
		detail = p.Err
	} else if len(p.Filtered) > 0 && p.IsFiles() {
		detail = p.Filtered[p.Cursor]
		if p.DetailPath == detail && p.Detail != "" {
			detail = p.Detail
		}
	}
	if p.Field != "app-workspace" || p.Loading || p.Err != "" {
		rows = append(rows, props.Styles.Muted.Render(cell.Line(detail, inner)))
	} else if len(p.Filtered) == 0 {
		rows = append(rows, props.Styles.Muted.Render("No matching folders"))
	}
	if p.Field == "app-workspace" {
		rows = append(rows, "", props.Styles.Muted.Render(strings.Repeat("─", inner)))
		button := "[ Select current folder ]"
		label := button + "  Ctrl+S"
		hits = append(hits, form.Hit{Index: -2, Y0: len(rows), Y1: len(rows) + 1, X0: 0, X1: len(button)})
		cancel := "[ Cancel ]"
		cancelX := len(label) + 3
		if cancelX+len(cancel) <= inner {
			hits = append(hits, form.Hit{Index: -4, Y0: len(rows), Y1: len(rows) + 1, X0: cancelX, X1: cancelX + len(cancel)})
			rows = append(rows, props.Styles.Focus.Render(label)+"   "+props.Styles.Muted.Render(cancel))
		} else {
			rows = append(rows, cell.Line(props.Styles.Focus.Render(label), inner))
			hits = append(hits, form.Hit{Index: -4, Y0: len(rows), Y1: len(rows) + 1, X0: 0, X1: len(cancel)})
			rows = append(rows, cancel)
		}
	}
	hint := "↑↓ select · Enter accept · Esc cancel"
	if p.IsFiles() {
		hint = "Enter open · Esc back · Ctrl+L path · Ctrl+D drives"
	}
	if p.Field == "app-workspace" {
		rows = append(rows, props.Styles.Muted.Render(cell.Line("↑↓ choose   Enter open   Esc cancel", inner)))
		hint = "Ctrl+L enter path   Ctrl+D drives"
		if p.Location {
			rows[len(rows)-1] = props.Styles.Muted.Render(cell.Line("Enter open path   Esc back to folders", inner))
		}
	}
	if p.Field == "app-workspace" {
		rows = append(rows, props.Styles.Muted.Render(cell.Line(hint, inner)))
	} else {
		rows = append(rows, popup.Footer(inner, hint, props.Styles.Muted)...)
		primary := "Select"
		if p.Browser {
			primary = "Open"
		}
		buttons := f.Model
		buttons.Index = -1
		if p.Control > 0 {
			buttons.Index = len(f.Fields) + p.Control - 1
		}
		actions, actionHits := buttons.Actions(inner, len(rows), primary, len(p.Filtered) > 0 && !p.Loading, false, props.formStyles())
		actionHits[0].Index, actionHits[1].Index = -7, -4
		hits = append(hits, actionHits...)
		rows = append(rows, actions)
	}
	return rows, hits, w, h
}

func (f *Model) ScanDirectory(root string) tea.Cmd {
	p := f.Picker
	p.Control = 0
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
	config := p.FileConfig
	accepts := func(path string) bool {
		if config == nil {
			return PickerAccepts(key, path)
		}
		for _, ext := range config.Extensions {
			if strings.EqualFold(filepath.Ext(path), ext) {
				return true
			}
		}
		return len(config.Extensions) == 0
	}
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
			} else if accepts(path) {
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
	if !p.IsFiles() || len(p.Filtered) == 0 || p.Loading {
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
