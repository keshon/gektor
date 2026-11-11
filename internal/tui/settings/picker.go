package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/keshon/gektor/internal/tui/components/selection"
)

// Picker owns one form's choice selection and editor state on the update loop.
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
	ID                 RequestID
	Field              string
	Items              []string
	Filtered           []string
	Search             textinput.Model
	selection.Model
	Loading bool
	Err     string
}

// IsFiles reports whether this picker browses filesystem paths.
func (p *Picker) IsFiles() bool { return p.FileConfig != nil || PickerFiles(p.Field) }

// PrimaryAction returns the explicit selection button label and availability.
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

// EnterAction reports the action of the currently focused picker control.
func (p *Picker) EnterAction() (string, bool) {
	if p.Control == 2 {
		return "Cancel", true
	}
	if p.Field == "app-workspace" && p.Control == 0 && !p.Location {
		return "Open", !p.Loading && p.Err == "" && len(p.Filtered) > 0
	}
	return p.PrimaryAction()
}

// OpenPicker opens choices for the focused field. Its command emits a local ScanMsg
// or a DiscoveryRequest for the parent to execute; it never calls backend hooks.
func (f *Model) OpenPicker(props Props) tea.Cmd {
	if f.Index < 0 || f.Index >= len(f.Fields) {
		return nil
	}

	key := f.Fields[f.Index].Key
	search := textinput.New()
	search.SetStyles(props.SearchStyles)
	search.Prompt = "Search: "
	search.SetVirtualCursor(true)
	search.Focus()
	search.SetWidth(max(props.Width-14, 12))

	p := &Picker{
		ID:     newRequestID(),
		Field:  key,
		Search: search,
	}
	f.Picker = p
	f.Editor.Blur()

	if key == "reasoning-effort" || key == "model" {
		return f.discoverPicker(key)
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

	root, err := f.pickerRoot(key)
	if err != nil {
		p.Err = err.Error()
		return nil
	}
	p.Browser = key == "app-workspace"
	p.Directory = root
	p.Loading = true
	if key == "app-workspace" {
		return f.ScanDirectory(root)
	}
	id := p.ID
	scan := catalogScan{ID: id, Root: root, Field: key}
	return scan.run
}

// Filter rebuilds visible choices and clamps the retained selection.
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

// AcceptScan accepts only the current request and replaces its loading/error state.
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

func (f *Model) pickerRoot(field string) (string, error) {
	selected := f.Editor.Value()
	if field == "app-workspace" {
		if selected != "" {
			return selected, nil
		}
		directory, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("find current folder: %w", err)
		}
		return directory, nil
	}
	if field == "load-profile" {
		executable, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("locate saved profiles: %w", err)
		}
		return filepath.Join(filepath.Dir(executable), "profiles"), nil
	}

	root := ""
	for _, field := range f.Fields {
		if field.Key == "model-root" {
			root = field.Value
		}
	}
	if root == "" {
		root = filepath.Dir(selected)
	}
	if root != "" && root != "." {
		return root, nil
	}

	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate model directory: %w", err)
	}
	return filepath.Join(filepath.Dir(executable), "assets", "models", "thinking"), nil
}
