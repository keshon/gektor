package settings

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/config"
)

// PickerKey handles picker navigation without submitting the underlying form.
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
		p.ID = newRequestID()
		p.Loading = true
		p.Search.SetValue("")
		id := p.ID
		scan := driveScan{ID: id}
		return scan.run

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
		p.ID = newRequestID()
		p.Loading = true
		p.Search.SetValue("")
		id := p.ID
		scan := catalogScan{ID: id, Root: root, Field: key}
		return scan.run
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

// UpdateInput edits the picker filter or location while retaining control ownership.
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
