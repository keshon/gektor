package settings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/popup"
)

type pickerLayout struct {
	rows                     []string
	hits                     []form.Hit
	inner, listTop, capacity int
}

// PickerRows returns rendering and hit regions from the same measured layout.
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
		label := p.itemLabel(p.Filtered[i])
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
	detail := p.detailText()
	rows = append(rows, props.Styles.Muted.Render(cell.Line(detail, inner)))

	action := p.actions()
	shortcuts := action.shortcuts
	primary, enabled, primaryHit := action.label, action.enabled, action.hit
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

func (p *Picker) itemLabel(item string) string {
	label := ChoiceLabel(p.Field, item)
	if !p.IsFiles() {
		return label
	}
	if p.Browser && filepath.Clean(item) == p.Parent && p.Parent != p.Directory {
		return "Parent folder"
	}
	if strings.HasSuffix(label, string(os.PathSeparator)) {
		clean := filepath.Clean(label)
		return "[Dir] " + filepath.Base(clean)
	}
	return filepath.Base(label)
}

func (p *Picker) detailText() string {
	if p.Loading {
		return "Loading…"
	}
	if p.Err != "" {
		return p.Err
	}
	if p.Field == "app-workspace" {
		if len(p.Filtered) == 0 {
			return "No matching folders"
		}
		return fmt.Sprintf("%d folders", len(p.Filtered))
	}
	if len(p.Filtered) == 0 {
		if p.Search.Value() != "" {
			return "No matching choices"
		}
		return "No choices available"
	}
	if p.IsFiles() {
		path := p.Filtered[p.Cursor]
		if p.DetailPath == path && p.Detail != "" {
			return p.Detail
		}
		return path
	}
	return fmt.Sprintf("%d choices", len(p.Filtered))
}

type pickerActions struct {
	label     string
	enabled   bool
	hit       int
	shortcuts []popup.Shortcut
}

func (p *Picker) actions() pickerActions {
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
	return pickerActions{label: primary, enabled: enabled, hit: primaryHit, shortcuts: shortcuts}
}
