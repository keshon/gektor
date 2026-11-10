package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/config"
	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/selection"
)

func (p *Picker) IsFiles() bool { return p.FileConfig != nil || PickerFiles(p.Field) }

func (p *Picker) PrimaryAction() (string, bool) {
	if p.Location {
		return "Open path", !p.Loading && strings.TrimSpace(p.Search.Value()) != ""
	}
	if p.Field == "app-workspace" {
		return "Select folder", !p.Loading && p.Err == "" && p.Directory != ""
	}
	label := "Select"
	if p.Browser && len(p.Filtered) > 0 && strings.HasSuffix(p.Filtered[p.Cursor], string(os.PathSeparator)) {
		label = "Open"
	}
	return label, !p.Loading && p.Err == "" && len(p.Filtered) > 0
}

func (p *Picker) EnterAction() (string, bool) {
	if p.Control == 2 {
		return "Cancel", true
	}
	if p.Field == "app-workspace" && p.Control == 0 && !p.Location {
		return "Open", !p.Loading && p.Err == "" && len(p.Filtered) > 0
	}
	return p.PrimaryAction()
}

type Picker struct {
	// Control is 0 for search/results, 1 for Select/Open, 2 for Cancel.
	Control            int
	FileConfig         *FilePicker
	Parent             string
	DetailPath, Detail string
	Location           bool
	LocationFilter     string
	LocationCursor     int
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
		profile := f.EditSession.Draft.Base()
		if fields, ok := f.EditSession.Draft.Fields("model"); ok {
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
	p.Err = ""
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
		if p.Field == "app-workspace" && !p.Location {
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
			p.Control = 0
			p.Search.SetValue(p.LocationFilter)
			p.Cursor = p.LocationCursor
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
		page := max(f.layoutPicker(props).capacity, 1)
		p.Move(-page, len(p.Filtered), page)
		return nil
	case "pgdown":
		page := max(f.layoutPicker(props).capacity, 1)
		p.Move(page, len(p.Filtered), page)
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
		return f.acceptPicker()
	case "ctrl+s":
		if p.Field == "app-workspace" && !p.Location && !p.Loading && p.Err == "" && p.Directory != "" {
			f.Editor.SetValue(p.Directory)
			f.Fields[f.Index].Value = p.Directory
			f.Picker = nil
		}
		return nil
	case "ctrl+r":
		return f.refreshPicker(props)
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
		if !p.Location {
			p.LocationFilter, p.LocationCursor = p.Search.Value(), p.Cursor
		}
		p.Location = true
		p.Control = 0
		p.Search.SetValue(p.Directory)
		return nil
	case "ctrl+d":
		if !p.IsFiles() {
			return nil
		}
		p.Browser = true
		p.Control = 0
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

func (f *Model) acceptPicker() tea.Cmd {
	p := f.Picker
	if p.Loading || (p.Err != "" && !p.Location) {
		return nil
	}
	if p.Location {
		if strings.TrimSpace(p.Search.Value()) == "" {
			return nil
		}
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
			f.EditSession.LoadedPath = value
			f.EditSession.Draft.Load(loaded)
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
}

func (f *Model) refreshPicker(props Props) tea.Cmd {
	p := f.Picker
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
}

func (p *Picker) UpdateInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	previous := p.Search.Value()
	p.Search, cmd = p.Search.Update(msg)
	if p.Search.Value() != previous && !p.Location {
		p.Cursor = 0
		p.Filter()
	}
	return cmd
}

type pickerLayout struct {
	rows                     []string
	hits                     []form.Hit
	inner, listTop, capacity int
}

func (f *Model) PickerRows(props Props) ([]string, []form.Hit, int, int) {
	l := f.layoutPicker(props)
	return l.rows, l.hits, props.Width, props.Height
}

func (f *Model) layoutPicker(props Props) pickerLayout {
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
	if h < 10 {
		hits := []form.Hit{{Index: -3, Y0: 0, Y1: 1, X0: max(inner-3, 0), X1: inner}}
		if h >= 7 {
			rows = append(rows, "Enlarge the terminal to browse")
		}
		hint := popup.Shortcuts(inner, popup.ShortcutStyles{Key: props.Surface.Normal.Bold(true), Action: props.Styles.Muted}, popup.Shortcut{Key: "Esc", Action: "cancel"})
		if h >= 6 {
			rows = append(rows, popup.Footer(inner, hint, lipgloss.NewStyle())...)
		} else {
			rows, hits = []string{hint}, nil
		}
		return pickerLayout{rows: rows, hits: hits, inner: inner}
	}
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
		if h >= 18 {
			rows = append(rows, "")
		}
		p.Search.Prompt = "Filter folders: "
		if p.Location {
			p.Search.Prompt = "Folder path: "
		}
	}
	searchRow := -1
	if p.Field != "theme" {
		p.Search.SetWidth(max(inner-cell.Width(p.Search.Prompt)-2, 1))
		searchRow = len(rows)
		rows = append(rows, cell.Line(p.Search.View(), inner))
	}
	if p.Field == "app-workspace" {
		if h >= 18 {
			rows = append(rows, "")
		}
	}
	listTop := len(rows)
	capacity := max(h-2-listTop-1-3, 0)
	start, end := p.Centered(len(p.Filtered), capacity)
	if capacity == 0 {
		start, end = 0, 0
	}
	body := []string{}
	hits := []form.Hit{{Index: -3, Y0: 0, Y1: 1, X0: max(inner-3, 0), X1: inner}}
	if searchRow >= 0 {
		hits = append(hits, form.Hit{Index: -8, Y0: searchRow, Y1: searchRow + 1, X1: inner})
	}
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
			if p.Control == 0 {
				style = props.Surface.Selected
			}
		}
		chosen := "  "
		if p.Filtered[i] == f.Editor.Value() {
			chosen = "✓ "
		}
		rowWidth := inner
		if len(p.Filtered) > capacity {
			rowWidth = max(inner-2, 1)
		}
		hits = append(hits, form.Hit{Index: -5, Option: i + 1, Y0: len(rows) + len(body), Y1: len(rows) + len(body) + 1, X1: rowWidth})
		body = append(body, style.Render(cell.Line(prefix+chosen+label, rowWidth)))
	}
	rows = append(rows, popup.ScrollRows(body, inner, start, len(p.Filtered), props.Surface.Normal)...)
	detail := fmt.Sprintf("%d choices", len(p.Filtered))
	if p.Loading {
		detail = "Loading…"
	} else if p.Err != "" {
		detail = p.Err
	} else if len(p.Filtered) == 0 {
		detail = "No choices available"
		if p.Search.Value() != "" {
			detail = "No matching choices"
		}
	} else if len(p.Filtered) > 0 && p.IsFiles() {
		detail = p.Filtered[p.Cursor]
		if p.DetailPath == detail && p.Detail != "" {
			detail = p.Detail
		}
	}
	if p.Field == "app-workspace" && !p.Loading && p.Err == "" {
		detail = fmt.Sprintf("%d folders", len(p.Filtered))
		if len(p.Filtered) == 0 {
			detail = "No matching folders"
		}
	}
	rows = append(rows, props.Styles.Muted.Render(cell.Line(detail, inner)))

	shortcuts := []popup.Shortcut{{Key: "Esc", Action: "cancel"}, {Key: "Enter", Action: "select"}, {Key: "↑↓", Action: "choose"}, {Key: "Tab", Action: "controls"}}
	primary, enabled := p.PrimaryAction()
	primaryHit := -7
	if p.Browser && len(p.Filtered) > 0 && strings.HasSuffix(p.Filtered[p.Cursor], string(os.PathSeparator)) {
		shortcuts[1].Action = "open"
	}
	if p.IsFiles() {
		shortcuts = append(shortcuts, popup.Shortcut{Key: "Ctrl+L", Action: "path"}, popup.Shortcut{Key: "Ctrl+D", Action: "drives"})
	}
	if p.Field == "app-workspace" {
		primaryHit = -2
		shortcuts = []popup.Shortcut{{Key: "Esc", Action: "cancel"}, {Key: "Enter", Action: "open"}, {Key: "Ctrl+S", Action: "select folder"}, {Key: "Ctrl+L", Action: "path"}, {Key: "Ctrl+D", Action: "drives"}, {Key: "Tab", Action: "controls"}}
	}
	if p.Location {
		primaryHit = -7
		shortcuts = []popup.Shortcut{{Key: "Esc", Action: "back"}, {Key: "Enter", Action: "open path"}, {Key: "Tab", Action: "controls"}}
	}
	if p.Err != "" {
		shortcuts = append([]popup.Shortcut{{Key: "Esc", Action: "cancel"}, {Key: "Ctrl+R", Action: "retry"}}, shortcuts[1:]...)
	}
	enterEnabled := len(p.Filtered) > 0 && !p.Loading && p.Err == ""
	if p.Location {
		enterEnabled = enabled
	}
	available := make([]popup.Shortcut, 0, len(shortcuts))
	for _, shortcut := range shortcuts {
		if shortcut.Key == "Enter" && !enterEnabled || shortcut.Key == "Ctrl+S" && !enabled {
			continue
		}
		available = append(available, shortcut)
	}
	shortcuts = available
	hint := popup.Shortcuts(inner, popup.ShortcutStyles{Key: props.Surface.Normal.Bold(true), Action: props.Styles.Muted}, shortcuts...)
	rows = append(rows, popup.Footer(inner, hint, lipgloss.NewStyle())...)
	buttons := f.Model
	buttons.Index = -1
	if p.Control > 0 {
		buttons.Index = len(f.Fields) + p.Control - 1
	}
	actions, actionHits := buttons.Actions(inner, len(rows), primary, enabled, false, props.formStyles())
	actionHits[0].Index, actionHits[1].Index = primaryHit, -4
	if !enabled {
		actionHits = actionHits[1:]
	}
	hits = append(hits, actionHits...)
	rows = append(rows, actions)
	return pickerLayout{rows: rows, hits: hits, inner: inner, listTop: listTop, capacity: capacity}
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
