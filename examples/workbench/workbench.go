package main

import (
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/form"
	"github.com/keshon/gektor/internal/tui/components/markdown"
	"github.com/keshon/gektor/internal/tui/components/pane"
	"github.com/keshon/gektor/internal/tui/components/popup"
	"github.com/keshon/gektor/internal/tui/components/table"
	"github.com/keshon/gektor/internal/tui/screen"
)

type book struct{ ID, Title, Notes string }

type page uint8

const (
	library page = iota
	details
	edit
)

// model owns the books and workers. Controls own draft editing and navigation;
// only their intent events change a book. Close cancels and joins owned workers.
type model struct {
	books        []book
	selected     string
	page         page
	controls     screen.Controls
	dialog       screen.DialogID
	rows         screen.TableID
	palette      *screen.Palette
	status       string
	pending      *refreshJob
	workers      sync.WaitGroup
	closed, quit bool
}

func newModel() *model {
	m := &model{
		books: []book{
			{ID: "cities", Title: "Invisible Cities", Notes: "Revisit the **conversations** between the cities.\n\n- Look for recurring images.\n- Keep a favorite passage."},
			{ID: "earthsea", Title: "A Wizard of Earthsea", Notes: "Read the next chapter this weekend."},
			{ID: "garden", Title: "The Secret Garden", Notes: "A book for a quiet afternoon."},
		},
		palette: screen.NewPalette("dark"),
		status:  "In memory only. Ctrl+R sorts the list; Ctrl+T changes theme.",
	}
	m.controls.Props = screen.ControlProps{Width: 80, Height: 24, Ready: true, ColorProfile: colorprofile.Unknown}
	m.prepare()
	m.rows = m.controls.NewTable()
	m.openLibrary()
	m.drainEvents()
	return m
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Close() {
	m.closed = true
	m.cancelRefresh()
	m.workers.Wait()
}

func (m *model) prepare() {
	p := &m.controls.Props
	p.Area = pane.Rect{Y: 2, Width: p.Width, Height: max(p.Height-3, 1)}
	p.Palette, p.Styles = m.palette, m.palette.Styles()
	p.Identity = "Reading list"
	p.Actions = []screen.Action{
		{Key: 'r', Mod: tea.ModCtrl, Name: "Ctrl+R", Label: "Refresh", Short: "Sort", Enabled: m.page == library},
		{Key: 't', Mod: tea.ModCtrl, Name: "Ctrl+T", Label: "Theme", Short: "Theme", Enabled: true},
		{Key: 'q', Mod: tea.ModCtrl, Name: "Ctrl+Q", Label: "Quit", Short: "Quit", Enabled: true},
	}
	m.controls.Prepare()
	m.controls.ResizeFormEditor()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.closed {
		return m, nil
	}

	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.controls.Props.Width, m.controls.Props.Height = msg.Width, msg.Height
		m.prepare()
		if m.page == details {
			m.refreshDetails()
		}
	case tea.ColorProfileMsg:
		m.controls.Props.ColorProfile = msg.Profile
	case refreshDone:
		if msg.job == m.pending {
			m.pending = nil
			msg.job.cancel()
			if msg.err == nil {
				m.books = msg.books
				m.status = fmt.Sprintf("Refreshed %d books, sorted by title.", len(m.books))
				m.refreshRows()
			}
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+q", "ctrl+c":
			m.quit = true
		case "ctrl+t":
			name := "light"
			if m.palette.Name == "light" {
				name = "dark"
			}
			m.palette = screen.NewPalette(name)
			m.prepare()
			if m.page == details {
				m.refreshDetails()
			}
		case "ctrl+r":
			if m.page == library {
				cmd = m.startRefresh()
			}
		default:
			cmd = m.controls.DialogInput(msg)
		}
	case tea.MouseMsg:
		mouse := msg.Mouse()
		if screen.IsMouseClick(msg) && mouse.Button == tea.MouseLeft && m.controls.ActionBarVisible() && mouse.Y == m.controls.Props.Height-1 {
			key, ok := m.controls.ToolbarKey(mouse.X)
			if ok {
				return m.Update(key)
			}
		}
		cmd = m.controls.DialogInput(msg)
	default:
		cmd = m.controls.DialogInput(msg)
	}

	m.drainEvents()
	m.prepare()
	if m.quit {
		m.cancelRefresh()
		return m, tea.Quit
	}
	return m, cmd
}

func (m *model) View() tea.View {
	p := m.controls.Props
	content := "\n" + cell.Line(m.status, p.Width)
	view := m.controls.Compose(tea.NewView(content))
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

func (m *model) openLibrary() {
	m.page = library
	m.controls.PresentDialog(screen.Dialog{
		Title: "Books",
		Table: &screen.Table{
			State:   m.rows,
			Columns: []table.Column{{Label: "Title"}},
			Actions: []table.Action{
				{ID: "details", Label: "Read", Key: "enter", Enabled: true},
				{ID: "close", Label: "Quit", Key: "esc", Enabled: true},
			},
		},
	})
	m.dialog = m.controls.DialogID()
	m.refreshRows()
}

func (m *model) refreshRows() {
	rows := make([]table.Row, 0, len(m.books))
	for _, book := range m.books {
		rows = append(rows, table.Row{ID: book.ID, Cells: []string{book.Title}})
	}
	m.controls.UpdateTable(m.dialog, "", rows, []table.Action{
		{ID: "details", Label: "Read", Key: "enter", Enabled: len(rows) > 0},
		{ID: "close", Label: "Quit", Key: "esc", Enabled: true},
	})
}

func (m *model) selectedBook() book {
	for _, book := range m.books {
		if book.ID == m.selected {
			return book
		}
	}
	return book{}
}

func (m *model) openDetails() {
	m.cancelRefresh()
	m.page = details
	m.controls.PresentDialog(screen.Dialog{Title: m.selectedBook().Title, Actions: []screen.DialogAction{{ID: "edit", Key: 'e', Label: "edit notes"}}})
	m.dialog = m.controls.DialogID()
	m.refreshDetails()
}

func (m *model) refreshDetails() {
	book := m.selectedBook()
	styles := m.palette.Styles()
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(m.palette.Color(screen.ColorPopupText)))
	width := popup.ModalWidth(m.controls.Props.Area.Width, 0) - 5
	lines := markdown.Render(book.Notes, width, markdown.Styles{
		Text: text, Muted: styles.Dim, Accent: styles.Hunk,
		Code: text, Keyword: styles.Focus, String: styles.Add,
		Number: styles.Gate, Comment: styles.Dim, Added: styles.Add, Deleted: styles.Del,
	})

	m.controls.UpdateDialog(m.dialog, book.Title, lines)
}

func (m *model) openEdit() {
	m.page = edit
	book := m.selectedBook()
	m.controls.PresentForm(screen.Form{
		Title:      "Edit book",
		ApplyLabel: "Save",
		Fields: []form.Field{
			{Key: "title", Label: "Title", Value: book.Title},
			{Key: "notes", Label: "Notes", Value: book.Notes, Multiline: true},
		},
	})
	m.dialog = m.controls.DialogID()
}

func (m *model) drainEvents() {
	for {
		events := m.controls.TakeEvents()
		if len(events) == 0 {
			return
		}
		for _, event := range events {
			m.handleEvent(event)
		}
	}
}

func (m *model) handleEvent(event screen.Event) {
	if event.ID != m.dialog {
		return
	}
	switch event.Kind {
	case screen.TableAction:
		m.selected = event.Row
		m.openDetails()
	case screen.DialogActionRequested:
		if event.Action == "edit" {
			m.openEdit()
		}
	case screen.Apply:
		title := strings.TrimSpace(event.Values["title"])
		if title == "" {
			m.controls.CompleteFormID(event.ID, fmt.Errorf("enter a title"))
			return
		}
		for i := range m.books {
			if m.books[i].ID == m.selected {
				m.books[i].Title, m.books[i].Notes = title, event.Values["notes"]
			}
		}
		m.controls.CompleteFormID(event.ID, nil)
	case screen.Closed:
		switch m.page {
		case library:
			m.quit = true
		case details:
			m.openLibrary()
		case edit:
			m.openDetails()
		}
	}
}
