package table

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/keshon/gektor/internal/tui/components/cell"
)

func fixture() (*Model, Props) {
	m := New(textinput.New().Styles())
	for i := range 40 {
		m.Rows = append(m.Rows, Row{ID: fmt.Sprint(i), Cells: []string{fmt.Sprintf("server-%02d", i), "On demand"}})
	}
	p := Props{Title: "Servers", Width: 100, Height: 24, DefaultAction: "details",
		Columns: []Column{{Label: "Name"}, {Label: "Status", Width: 15}},
		Actions: []Action{{ID: "details", Label: "Details", Key: "enter", Enabled: true},
			{ID: "delete", Label: "Delete", Key: "delete"}, {ID: "close", Label: "Close", Key: "esc", Enabled: true}}}
	return m, p
}

func TestFilteringAndNavigationNeverActivateActions(t *testing.T) {
	m, p := fixture()
	m.Update(tea.KeyPressMsg{Code: '/'}, p)
	action, _ := m.Update(tea.PasteMsg{Content: "server-23"}, p)
	if action != "" || m.Selected() != "23" {
		t.Fatal("filter activated an action or lost row identity")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, p)
	if m.Focus != 0 || m.Search.Value() != "server-23" {
		t.Fatal("leaving filter discarded the query")
	}
	action, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDelete}, p)
	if action != "" {
		t.Fatal("disabled action accepted a shortcut")
	}
	action, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}, p)
	if action != "details" {
		t.Fatal("Enter did not request Details")
	}
}

func TestMouseUsesRenderedRowsAndIgnoresOutsideWheel(t *testing.T) {
	m, p := fixture()
	l := m.Layout(p)
	x, y := max((p.Width-l.Width)/2, 0)+2, max((p.Height-len(l.Rows)-2)/2, 0)+1
	for _, hit := range l.Hits {
		if hit.Action == "row" && hit.Row == 1 {
			if action := m.Mouse(tea.MouseClickMsg{X: x + hit.X0, Y: y + hit.Y, Button: tea.MouseLeft}, p); action != "" || m.Selected() != "1" {
				t.Fatal("row click accepted or missed its row")
			}
		}
	}
	m.Mouse(tea.MouseWheelMsg{X: 0, Y: 0, Button: tea.MouseWheelDown}, p)
	if m.Selected() != "1" {
		t.Fatal("outside scroll changed the list")
	}
	m.Mouse(tea.MouseWheelMsg{X: x + 2, Y: y + l.BodyTop, Button: tea.MouseWheelDown}, p)
	if m.Selected() != "2" {
		t.Fatal("wheel over body did not navigate")
	}
	m.Rows, m.Cursor, m.Offset = m.Rows[:2], 0, 0
	l = m.Layout(p)
	y = max((p.Height-len(l.Rows)-2)/2, 0) + 1
	m.Mouse(tea.MouseWheelMsg{X: x + 2, Y: y + l.BodyTop + l.BodyHeight, Button: tea.MouseWheelDown}, p)
	if m.Selected() != "0" {
		t.Fatal("wheel over footer moved a short list")
	}
}

func TestTableResizePreservesDraftAndClosePath(t *testing.T) {
	m, p := fixture()
	m.Search.SetValue("server")
	for _, size := range [][2]int{{40, 12}, {80, 20}, {120, 40}} {
		p.Width, p.Height = size[0], size[1]
		l := m.Layout(p)
		if len(l.Rows)+2 > p.Height {
			t.Fatal("table exceeds available height", size)
		}
		for _, row := range l.Rows {
			if cell.Width(row) > l.Width-4 {
				t.Fatal("row exceeds frame", size, ansi.Strip(row))
			}
			if strings.Contains(ansi.Strip(row), "Filter:") && strings.Contains(ansi.Strip(row), "…") {
				t.Fatal("empty filter was clipped by the frame")
			}
		}
		if m.Search.Value() != "server" {
			t.Fatal("resize lost filter")
		}
		if action, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape}, p); action != "close" {
			t.Fatal("close is unavailable")
		}
	}
	p.Width, p.Height = 40, 8
	l := m.Layout(p)
	if !strings.Contains(strings.Join(l.Rows, "\n"), "Enlarge") {
		t.Fatal("small terminal lacks a size message")
	}
	m.Update(tea.KeyPressMsg{Code: '/'}, p)
	m.Update(tea.PasteMsg{Content: "hidden input"}, p)
	if m.Search.Value() != "server" || m.Search.Focused() {
		t.Fatal("compact size message left a hidden filter active")
	}
}
