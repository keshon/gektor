package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/screen"
)

func key(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func TestEditValidatesThenSavesThroughControlIntents(t *testing.T) {
	m := newModel()
	defer m.Close()
	m.Update(key(tea.KeyEnter, 0))
	m.Update(key('e', 0))
	if m.page != edit {
		t.Fatal("read then edit did not open the form")
	}

	m.Update(key('g', tea.ModCtrl))
	m.Update(key(tea.KeyBackspace, 0))
	m.Update(key('s', tea.ModCtrl))
	if m.page != edit || !strings.Contains(ansi.Strip(m.View().Content), "enter a title") {
		t.Fatal("empty title did not retain the form and show validation")
	}
	if m.books[0].Title != "Invisible Cities" {
		t.Fatal("invalid draft changed the book")
	}

	m.Update(tea.PasteMsg{Content: "Cities revisited"})
	m.Update(key(tea.KeyTab, 0))
	m.Update(key('g', tea.ModCtrl))
	m.Update(tea.PasteMsg{Content: "Remember **the bridges**."})
	m.Update(key('t', tea.ModCtrl))
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m.Update(key('s', tea.ModCtrl))
	if m.page != details || m.books[0].Title != "Cities revisited" || m.books[0].Notes != "Remember **the bridges**." {
		t.Fatalf("save lost the draft across theme/resize: page %d, book %#v", m.page, m.books[0])
	}
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "the bridges") || strings.Contains(view, "**the bridges**") {
		t.Fatal("detail notes were not rendered as Markdown")
	}

	m.Update(key(tea.KeyEscape, 0))
	if m.page != library || !strings.Contains(ansi.Strip(m.View().Content), "Cities revisited") {
		t.Fatal("return to list did not show the saved title")
	}
}

func TestCancelAndStaleDialogCannotSave(t *testing.T) {
	m := newModel()
	defer m.Close()
	m.Update(key(tea.KeyEnter, 0))
	m.Update(key('e', 0))
	oldDialog := m.dialog
	m.Update(key('g', tea.ModCtrl))
	m.Update(tea.PasteMsg{Content: "Unsaved title"})
	m.Update(key(tea.KeyEscape, 0))
	m.handleEvent(screen.Event{Kind: screen.Apply, ID: oldDialog, Values: map[string]string{"title": "Stale title"}})
	if m.page != details || m.books[0].Title != "Invisible Cities" {
		t.Fatal("cancelled or replaced draft mutated the book")
	}
}

func TestRefreshRejectsStaleResultAndPreservesSelection(t *testing.T) {
	m := newModel()
	defer m.Close()
	selected := m.controls.TableSelection(m.rows)
	first := m.startRefresh()
	oldJob := m.pending
	second := m.startRefresh()
	currentJob := m.pending
	m.Update(refreshDone{job: oldJob, books: []book{{ID: "stale", Title: "Stale"}}})
	if m.pending != currentJob || len(m.books) != 3 {
		t.Fatal("replaced refresh overwrote the current list")
	}
	first()
	m.Update(second())
	if m.books[0].ID != "earthsea" || m.controls.TableSelection(m.rows) != selected || m.pending != nil {
		t.Fatalf("refresh lost order or selection: %#v, selected %q", m.books, m.controls.TableSelection(m.rows))
	}

	refresh := m.startRefresh()
	job := m.pending
	m.Update(key(tea.KeyEnter, 0))
	if m.pending != nil || m.page != details {
		t.Fatal("opening a book did not cancel the refresh")
	}
	m.books[0].Title = "New title"
	m.Update(refreshDone{job: job, books: []book{{ID: "stale"}}})
	refresh()
	if m.books[0].Title != "New title" {
		t.Fatal("cancelled refresh overwrote a later edit")
	}
}

func TestCloseJoinsWorkerWithoutExecutingCommand(t *testing.T) {
	m := newModel()
	cmd := m.startRefresh()
	m.Close()
	msg := cmd().(refreshDone)
	if !errors.Is(msg.err, context.Canceled) {
		t.Fatalf("worker was not cancelled: %v", msg.err)
	}
	m.Update(msg)
	if m.pending != nil || len(m.books) != 3 {
		t.Fatal("closed model accepted a worker result")
	}
}

func TestIndependentPaletteAndTerminalBounds(t *testing.T) {
	other := newModel()
	defer other.Close()
	for _, theme := range []string{"dark", "light"} {
		for _, size := range [][2]int{{40, 14}, {60, 20}, {80, 24}, {120, 32}} {
			t.Run(fmt.Sprintf("%s/%dx%d", theme, size[0], size[1]), func(t *testing.T) {
				m := newModel()
				defer m.Close()
				if theme == "light" {
					m.Update(key('t', tea.ModCtrl))
				}
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				for _, next := range []tea.Msg{nil, key(tea.KeyEnter, 0), key('e', 0)} {
					if next != nil {
						m.Update(next)
					}
					rows := strings.Split(m.View().Content, "\n")
					if len(rows) != size[1] {
						t.Fatalf("page %d height = %d", m.page, len(rows))
					}
					for i, row := range rows {
						if ansi.StringWidth(row) != size[0] {
							t.Fatalf("page %d row %d width = %d", m.page, i, ansi.StringWidth(row))
						}
					}
				}
			})
		}
	}
	if other.palette.Name != "dark" {
		t.Fatal("changing one app's theme changed another app")
	}
}

func TestProgramQuitsThroughDriver(t *testing.T) {
	m := newModel()
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(strings.NewReader("\x11")), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithWindowSize(80, 24))
	_, err := p.Run()
	if err != nil {
		t.Fatalf("running terminal driver: %v", err)
	}
	if !m.quit {
		t.Fatal("terminal driver did not route Ctrl+Q")
	}
}

func TestToolbarMouseUsesControlLayout(t *testing.T) {
	m := newModel()
	defer m.Close()
	for _, cell := range m.controls.FooterCells() {
		if cell.Action.Key != 't' {
			continue
		}
		m.Update(tea.MouseClickMsg{X: cell.Start, Y: m.controls.Props.Height - 1, Button: tea.MouseLeft})
		if m.palette.Name != "light" {
			t.Fatal("clicking the theme shortcut did not change theme")
		}
		return
	}
	t.Fatal("theme shortcut is missing")
}

func TestDetailResizeAndThemePreserveLifetimeAndScroll(t *testing.T) {
	m := newModel()
	defer m.Close()
	var notes strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&notes, "paragraph %03d\n\n", i)
	}
	m.books[0].Notes = notes.String()
	m.Update(key(tea.KeyEnter, 0))
	id := m.dialog
	for i := 0; i < 12; i++ {
		m.Update(key(tea.KeyDown, 0))
	}
	before := ansi.Strip(m.View().Content)
	if !strings.Contains(before, "paragraph 006") {
		t.Fatalf("detail did not scroll: %s", before)
	}
	m.Update(key('t', tea.ModCtrl))
	after := ansi.Strip(m.View().Content)
	if m.dialog != id || !strings.Contains(after, "paragraph 006") || strings.Contains(after, "paragraph 000") {
		t.Fatal("theme reset detail scroll or lifetime")
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	after = ansi.Strip(m.View().Content)
	if m.dialog != id || !strings.Contains(after, "paragraph 006") || strings.Contains(after, "paragraph 000") {
		t.Fatal("resize reset detail scroll or lifetime")
	}
}

func TestLibraryNavigationSurvivesDetailAndCancelledEdit(t *testing.T) {
	m := newModel()
	defer m.Close()
	m.books = make([]book, 60)
	for i := range m.books {
		m.books[i] = book{ID: fmt.Sprint(i), Title: fmt.Sprintf("Book record %02d", i), Notes: "Notes"}
	}
	m.openLibrary()
	m.drainEvents()
	m.Update(key('/', 0))
	m.Update(tea.PasteMsg{Content: "record"})
	m.Update(key(tea.KeyEsc, 0))
	m.Update(key(tea.KeyPgDown, 0))
	m.Update(key(tea.KeyPgDown, 0))
	before := m.controls.TableSnapshot(m.dialog)
	if before.Offset == 0 || before.Filter != "record" {
		t.Fatal("library did not scroll or filter")
	}
	m.Update(key(tea.KeyEnter, 0))
	if m.page != details || m.selected != before.Selected {
		t.Fatal("selected record did not open")
	}
	m.Update(key('e', 0))
	m.Update(key('g', tea.ModCtrl))
	m.Update(tea.PasteMsg{Content: "cancelled title"})
	m.Update(key(tea.KeyEsc, 0))
	m.Update(key(tea.KeyEsc, 0))
	after := m.controls.TableSnapshot(m.dialog)
	if m.page != library || after.Selected != before.Selected || after.Filter != before.Filter || after.Offset != before.Offset || after.Focus != before.Focus {
		t.Fatal("return from edit/detail reset library navigation", before, after)
	}
	for _, book := range m.books {
		if book.Title == "cancelled title" {
			t.Fatal("cancelled edit changed a book")
		}
	}
}
