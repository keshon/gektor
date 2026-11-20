package screen

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/keshon/gektor/pkg/tui/components/cell"
	fieldform "github.com/keshon/gektor/pkg/tui/components/fields"
	"github.com/keshon/gektor/pkg/tui/components/form"
	"github.com/keshon/gektor/pkg/tui/components/pane"
	"github.com/keshon/gektor/pkg/tui/components/popup"
	"github.com/keshon/gektor/pkg/tui/components/table"
)

// DialogView renders the current overlay inside its available rectangle.
func (m *Controls) DialogView() string {
	if m.dialog.Form != nil {
		return m.FormView()
	}
	rows, width, height := m.DialogRows()
	return popup.FrameBox(rows, width, height, m.dialogWidth(width), m.popupSurface())
}

// OverlayDialog paints the current overlay over the supplied pane body.
func (m *Controls) OverlayDialog(background string) string {
	if m.dialog.Form != nil {
		return m.dialog.Form.Overlay(background, m.FormProps())
	}
	rows, width, height := m.DialogRows()
	return popup.OverlayBox(background, rows, width, height, m.dialogWidth(width), m.popupSurface())
}

// DialogRows fits the open table or information overlay without changing scroll state.
func (m *Controls) DialogRows() ([]string, int, int) {
	if m.dialog.Table != nil {
		p := m.TableProps()
		return m.dialog.Table.model.Layout(p).Rows, p.Width, p.Height
	}
	layout := m.LayoutDialog()
	offset := min(max(m.dialog.Offset, 0), max(len(layout.Lines)-layout.Capacity, 0))
	rows := layout.Rows
	body := []string{}
	for _, line := range layout.Lines[offset:min(offset+layout.Capacity, len(layout.Lines))] {
		body = append(body, cell.Line(line, layout.Inner))
	}
	rows = append(rows, popup.ScrollRows(body, layout.Inner, offset, len(layout.Lines), m.popupSurface().Normal)...)
	if len(layout.Lines) > layout.Capacity {
		rows = append(rows, m.Props.Styles.Dim.Render(strconv.Itoa(offset+1)+"–"+strconv.Itoa(min(offset+layout.Capacity, len(layout.Lines)))+" / "+strconv.Itoa(len(layout.Lines))))
	}
	rows = append(rows, popup.Footer(layout.Inner, layout.Hint, lipgloss.NewStyle())...)
	return rows, layout.Width, layout.Height
}

// ScrollDialog clamps an information overlay to its scrollable range.
func (m *Controls) ScrollDialog(delta int) {
	if m.dialog == nil || m.dialog.Form != nil || m.dialog.Table != nil {
		return
	}
	layout := m.LayoutDialog()
	m.dialog.Offset = min(max(m.dialog.Offset+delta, 0), max(len(layout.Lines)-layout.Capacity, 0))
}

// LayoutDialog measures the open information overlay for painting and hit testing.
func (m *Controls) LayoutDialog() DialogLayout {
	d := m.dialog
	availW, availH := m.Props.Width, m.Props.Height-3
	if m.Props.Ready {
		_, _, availW, availH = m.Area()
	}
	inner := max(m.dialogWidth(availW)-4, 1)
	// Info dialog: title, lines, close hint (with help tabs if applicable).
	rows := popup.Header(cell.Truncate(d.Title, inner), inner, m.Props.Styles.Title)
	lines := []string{}
	for _, line := range d.Lines {
		lines = append(lines, cell.Wrap(cleanText(line), inner-1)...)
	}
	shortcuts := []popup.Shortcut{{Key: "Esc/Enter", Action: "close"}, {Key: "F11", Action: "expand"}}
	for _, action := range d.Actions {
		shortcuts = append(shortcuts, popup.Shortcut{Key: strings.ToUpper(string(action.Key)), Action: action.Label})
	}
	var tabs pane.TabLayout
	if d.Help && len(m.Props.Help) > 0 {
		sections := m.Props.Help
		page := min(max(d.Page, 0), len(sections)-1)
		labels := make([]string, len(sections))
		for i, section := range sections {
			labels[i] = section.Title
		}
		if len(sections) > 1 {
			tabs = pane.Tabs(labels, page, inner, true, pane.TabStyles{Normal: m.Props.Styles.Dim, Active: m.Props.Styles.Title, Focus: m.Props.Styles.Focus})
			rows = append(rows, tabs.Rows...)
		}

		if availH >= 10 {
			rows = append(rows, "")
		}
		lines = m.HelpRows(sections[page], max(inner-1, 1))
		shortcuts = []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "Tab/←→", Action: "category"}, {Key: "↑↓", Action: "scroll"}}
		if len(sections) == 1 {
			shortcuts = []popup.Shortcut{{Key: "Esc", Action: "close"}, {Key: "↑↓", Action: "scroll"}}
		}
	}
	hint := popup.Shortcuts(inner, m.ShortcutStyles(), shortcuts...)
	if len(d.Shortcuts) > 0 && !d.Help {
		hint = popup.Shortcuts(inner, m.ShortcutStyles(), d.Shortcuts...)
	}
	capacity := max(availH-2-len(rows)-3, 1)
	// Reserve an extra row for the scroll-position indicator when needed.
	if len(lines) > capacity {
		capacity = max(capacity-1, 1)
	}
	return DialogLayout{Rows: rows, Lines: lines, Hint: hint, Inner: inner,
		Capacity: capacity, Width: availW, Height: availH, Tabs: tabs}
}

// FormView renders the open form using the shared overlay geometry.
func (m *Controls) FormView() string {
	return m.dialog.Form.View(m.FormProps())
}

// UpdateFormInput routes paste and editor messages to the open form or table; other overlays ignore them.
func (m *Controls) UpdateFormInput(msg tea.Msg) tea.Cmd {
	if m.dialog != nil && m.dialog.Secret != nil {
		return m.SecretInput(msg)
	}
	if m.dialog != nil && m.dialog.Table != nil {
		return m.TableInput(msg)
	}
	if m.dialog == nil || m.dialog.Form == nil {
		return nil
	}
	return m.dialog.Form.UpdateInput(msg)
}

// FocusForm selects a visible field or button and refits its editor.
func (m *Controls) FocusForm(index int) {
	f := m.dialog.Form
	f.Focus(index, m.FieldVisible)

	m.ResizeFormEditor()
}

// OpenForm initializes draft editing; confirmation emits an Apply event.
func (m *Controls) OpenForm(title, hint, applyLabel string, fields []form.Field) {
	m.Open(title, nil)
	f := &formState{Model: fieldform.NewModel(fields, m.NewEditor())}
	f.Hint, f.ApplyLabel = hint, applyLabel
	f.Editor.SetVirtualCursor(true)
	f.Editor.DynamicHeight = false
	f.Editor.Prompt = ""
	f.Editor.CharLimit = 65536
	m.dialog.Form = f
	m.FocusForm(0)
}

// OpenRemoval opens a destructive confirmation with Cancel initially selected.
func (m *Controls) OpenRemoval(title, hint, label string, fields []form.Field) {
	m.OpenForm(title, hint, label, fields)
	m.dialog.Form.Destructive = true
	m.FocusForm(len(fields) + 1)
}

// ApplyForm closes on success and keeps validation errors beside the current draft.
func (m *Controls) ApplyForm() {
	f := m.dialog.Form
	if f.DisablePrimary {
		return
	}
	m.events = append(m.events, Event{Kind: Apply, ID: m.DialogID(), Values: f.Values()})
}

// FormKey interprets a key in the open form and applies any resulting intent.
func (m *Controls) FormKey(msg tea.KeyPressMsg) tea.Cmd {
	intent, cmd := m.dialog.Form.UpdateKey(msg, m.FormProps())
	m.ResizeFormEditor()
	return tea.Batch(cmd, m.HandleIntent(intent))
}

// HandleIntent dispatches form confirmation, cancellation, or Save Copy on the update loop.
func (m *Controls) HandleIntent(intent fieldform.Intent) tea.Cmd {
	switch intent.Action {
	case fieldform.Apply:
		m.ApplyForm()
	case fieldform.Cancel:
		m.Close()
	case fieldform.SaveCopy:
		if m.dialog.Form.SaveCopy {
			m.events = append(m.events, Event{Kind: SaveCopy, ID: m.DialogID()})
		}
	}
	return nil
}

// FormMouse uses the same form layout for pointer input and painting.
func (m *Controls) FormMouse(msg tea.MouseMsg) tea.Cmd {
	intent, cmd := m.dialog.Form.Mouse(msg, m.FormProps())
	m.ResizeFormEditor()
	return tea.Batch(cmd, m.HandleIntent(intent))
}

// ResizeFormEditor refits the active field after a resize or focus change.
func (m *Controls) ResizeFormEditor() {
	if m.dialog == nil || m.dialog.Form == nil {
		return
	}
	f := m.dialog.Form
	f.Editor.SetWidth(max(popup.ModalWidth(m.Props.Area.Width, 0)-5, 1))
	height := 1
	if f.Index < len(f.Fields) && f.Fields[f.Index].Multiline {
		height = min(3, max(m.Props.Area.Height-10, 1))
	}
	f.Editor.SetHeight(height)
}

// FieldVisible reports whether the open form currently displays a field.
func (m *Controls) FieldVisible(index int) bool { return m.dialog.Form.FieldVisible(index) }

// FormRows returns rendered rows and the matching control hit regions.
func (m *Controls) FormRows() ([]string, []form.Hit, int, int) {
	return m.dialog.Form.Rows(m.FormProps())
}

// TableProps reads the prepared table snapshot and shared bounds.
func (m *Controls) TableProps() table.Props {
	d := m.dialog.Table
	_, _, width, height := m.Area()
	return table.Props{Title: m.dialog.Title, Description: d.Description, EmptyMessage: d.EmptyMessage, Width: width, Height: height, DefaultAction: "details", PreferredWidth: m.dialogWidth(width), List: d.List,
		Columns: d.Columns, Actions: d.Actions, Surface: m.popupSurface(),
		Styles: form.Styles{Normal: m.popupSurface().Normal, Title: m.Props.Styles.Title, Muted: m.Props.Styles.Dim,
			Focus: m.Props.Styles.Focus, Disabled: m.Props.Styles.Disabled, Error: m.Props.Styles.Err}}
}

// TableInput handles table navigation, filtering, and stable-ID activation.
func (m *Controls) TableInput(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "f11" {
		m.dialog.Maximized = !m.dialog.Maximized
		return nil
	}
	model := m.dialog.Table.model
	model.Search.SetStyles(m.palette().TextInputStyles(model.Search.Styles()))
	props := m.TableProps()
	action, cmd := model.Update(msg, props)
	m.events = append(m.events, Event{Kind: TableChanged, ID: m.DialogID()})
	return tea.Batch(cmd, m.TableAction(action))
}

// TableAction records an activation with the selected stable row ID.
func (m *Controls) TableAction(action string) tea.Cmd {
	if action == "" {
		return nil
	}
	if action == "close" {
		m.Close()
		return nil
	}
	d := m.dialog.Table
	id := d.model.Selected()
	m.events = append(m.events, Event{Kind: TableAction, ID: m.DialogID(), Action: action, Row: id})
	return nil
}

// NewTable creates reusable navigation state owned by these controls. The handle
// retains filtering, selection, focus and scrolling across detail/edit overlays.
func (m *Controls) NewTable() TableID {
	return TableID{owner: m, model: table.New(m.palette().TextInputStyles(textinput.New().Styles()))}
}

// DialogScrollbarMouse retains thumb ownership until release, including outside the scrollbar.
func (m *Controls) DialogScrollbarMouse(msg tea.MouseMsg) bool {
	d := m.dialog
	if d == nil || d.Form != nil || d.Table != nil {
		return false
	}
	l := m.LayoutDialog()
	rows, _, _ := m.DialogRows()
	left, _, _, _ := m.Area()
	x := left + max((l.Width-m.dialogWidth(l.Width))/2, 0) + 2 + l.Inner - 1
	y := m.Props.Area.Y + max((l.Height-len(rows)-2)/2, 0) + 1 + len(l.Rows)
	track := popup.ScrollTrack{X: x, Y: y, Height: l.Capacity, Total: len(l.Lines), Offset: d.Offset}
	offset, handled := d.Drag.Mouse(msg, track)
	if handled {
		d.Offset = offset
	}
	return handled
}

// InfoKey consumes keys for the open information overlay; the shell handles application quit policy.
func (m *Controls) InfoKey(msg tea.KeyPressMsg) tea.Cmd {
	m.dialog.Drag = popup.ScrollDrag{}
	if m.dialog.Secret != nil {
		return m.SecretInput(msg)
	}
	for _, action := range m.dialog.Actions {
		if strings.EqualFold(msg.String(), string(action.Key)) {
			m.events = append(m.events, Event{Kind: DialogActionRequested, ID: m.DialogID(), Action: action.ID})
			return nil
		}
	}
	// Takeover: the dialog eats every key but close and quit so
	// typing can neither reach the input nor toggle state behind it.
	// Close is esc/enter only: "q" must stay typable for dialogs
	// with inputs tomorrow. Confirmations are answered above, before
	// this branch; an info dialog has nothing to confirm.
	switch msg.String() {
	case "f11":
		m.dialog.Maximized = !m.dialog.Maximized
		return nil
	case "n", "N", "esc":
		m.Close()
		return nil
	case "enter":
		m.Close()
		return nil
	case "ctrl+c":
		return tea.Quit
	}
	if !m.dialog.Help {
		switch msg.String() {
		case "down":
			m.ScrollDialog(1)
		case "up":
			m.ScrollDialog(-1)
		case "pgdown":
			m.ScrollDialog(max(m.Props.Area.Height/2, 1))
		case "pgup":
			m.ScrollDialog(-max(m.Props.Area.Height/2, 1))
		}
	}
	if m.dialog.Help && len(m.Props.Help) > 0 {
		switch msg.String() {
		case "tab", "right":
			m.dialog.Page = (m.dialog.Page + 1) % len(m.Props.Help)
			m.dialog.Offset = 0
		case "shift+tab", "left":
			m.dialog.Page = (m.dialog.Page + len(m.Props.Help) - 1) % len(m.Props.Help)
			m.dialog.Offset = 0
		case "down":
			m.ScrollDialog(1)
		case "up":
			m.ScrollDialog(-1)
		case "pgdown":
			m.ScrollDialog(max(m.Props.Area.Height/2, 1))
		case "pgup":
			m.ScrollDialog(-max(m.Props.Area.Height/2, 1))
		}
	}
	return nil
}

func (m *Controls) dialogWidth(area int) int {
	preferred := m.dialog.PreferredWidth
	if m.dialog.Maximized {
		preferred = area
	}
	return popup.ModalWidth(area, preferred)
}

// DialogInput routes overlay input without requiring hosts to inspect its type.
// Application-wide shortcuts and quit policy remain the host's responsibility.
func (m *Controls) DialogInput(msg tea.Msg) tea.Cmd {
	if m.dialog == nil {
		return nil
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		return m.DialogMouse(mouse)
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case m.dialog.Secret != nil:
			return m.SecretInput(key)
		case m.dialog.Table != nil:
			return m.TableInput(key)
		case m.dialog.Form != nil:
			return m.FormKey(key)
		default:
			return m.InfoKey(key)
		}
	}
	return m.UpdateFormInput(msg)
}
