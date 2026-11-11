package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ScanDirectory starts a new request with a copied extension filter; execute the
// returned command outside the update loop and route its ScanMsg back to the form.
func (f *Model) ScanDirectory(root string) tea.Cmd {
	p := f.Picker
	p.Control = 0
	absolute, err := filepath.Abs(root)
	if err == nil {
		root = absolute
	}

	root = filepath.Clean(root)
	p.Parent = filepath.Dir(root)
	p.ID = newRequestID()
	p.Directory = root
	p.Cursor = 0
	p.Loading = true
	p.Err = ""
	p.Items = nil
	p.Filter()
	p.Search.SetValue("")

	scan := directoryScan{
		ID:    p.ID,
		Root:  root,
		Field: p.Field,
	}
	if p.FileConfig != nil {
		scan.Custom = true
		scan.Extensions = slices.Clone(p.FileConfig.Extensions)
	}
	return scan.run
}

// DetailCommand captures the selected path for an asynchronous preview, or returns nil
// when that preview is already requested or the picker has no file selection.
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
	scan := detailScan{
		ID:    p.ID,
		Path:  path,
		Field: p.Field,
	}
	return scan.run
}

// DetailMsg carries one path preview back to its originating picker request.
type DetailMsg struct {
	ID         RequestID
	Path, Text string
}

// AcceptDetail ignores previews for a replaced request or a different selected path.
func (f *Model) AcceptDetail(msg DetailMsg) {
	if f == nil || f.Picker == nil {
		return
	}
	p := f.Picker
	if p.ID == msg.ID && p.DetailPath == msg.Path {
		p.Detail = msg.Text
	}
}

type directoryScan struct {
	ID         RequestID
	Root       string
	Field      string
	Custom     bool
	Extensions []string
}

func (s directoryScan) accepts(path string) bool {
	if !s.Custom {
		return PickerAccepts(s.Field, path)
	}
	if len(s.Extensions) == 0 {
		return true
	}
	for _, extension := range s.Extensions {
		if strings.EqualFold(filepath.Ext(path), extension) {
			return true
		}
	}
	return false
}

func (s directoryScan) run() tea.Msg {
	entries, err := os.ReadDir(s.Root)
	items := []string{}
	parent := filepath.Dir(s.Root)
	if parent != s.Root {
		items = append(items, parent+string(os.PathSeparator))
	}

	for _, entry := range entries {
		path := filepath.Join(s.Root, entry.Name())
		if entry.IsDir() {
			items = append(items, path+string(os.PathSeparator))
			continue
		}
		if s.accepts(path) {
			items = append(items, path)
		}
	}
	return ScanMsg{ID: s.ID, Items: items, Err: err}
}

type catalogScan struct {
	ID    RequestID
	Root  string
	Field string
}

func (s catalogScan) run() tea.Msg {
	items, err := CatalogFiles(s.Root, s.Field)
	return ScanMsg{ID: s.ID, Items: items, Err: err}
}

type driveScan struct{ ID RequestID }

func (s driveScan) run() tea.Msg {
	items := []string{}
	for letter := 'C'; letter <= 'Z'; letter++ {
		root := fmt.Sprintf("%c:%c", letter, os.PathSeparator)
		info, err := os.Stat(root)
		if err == nil && info.IsDir() {
			items = append(items, root)
		}
	}
	return ScanMsg{ID: s.ID, Items: items}
}

type detailScan struct {
	ID    RequestID
	Path  string
	Field string
}

func (s detailScan) run() tea.Msg {
	text := CatalogDetail(s.Field, s.Path)
	if s.Field == "model-file" {
		info, err := os.Stat(s.Path)
		if err == nil && !info.IsDir() {
			text = fmt.Sprintf("%.2f GiB · %s", float64(info.Size())/(1<<30), text)
		}
	}
	return DetailMsg{ID: s.ID, Path: s.Path, Text: text}
}
