package table

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/keshon/gektor/internal/tui/components/cell"
	"github.com/keshon/gektor/internal/tui/components/popup"
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

func TestScrollbarDragUsesDrawnTrackAndNeverActivatesActions(t *testing.T) {
	for _, list := range []bool{false, true} {
		for _, width := range []int{40, 100, 140} {
			m, p := fixture()
			p.List, p.Width = list, width
			l := m.Layout(p)
			x := max((p.Width-l.Width)/2, 0) + 2 + l.Scroll.X
			y := max((p.Height-len(l.Rows)-2)/2, 0) + 1 + l.Scroll.Y
			if !strings.HasSuffix(ansi.Strip(l.Rows[l.BodyTop]), "█") {
				t.Fatal("thumb does not match drawn column")
			}
			m.Mouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}, p)
			if !m.scroll.Active {
				t.Fatal("thumb did not capture mouse", width, list)
			}
			m.Mouse(tea.MouseMotionMsg{X: 0, Y: p.Height + 10, Button: tea.MouseLeft}, p)
			action := m.Mouse(tea.MouseReleaseMsg{X: 0, Y: p.Height + 10}, p)
			if action != "" || m.scroll.Active || m.Offset != len(m.Rows)-l.Capacity {
				t.Fatal("drag did not reach bottom and release", width, list, m.Offset)
			}
			l = m.Layout(p)
			top, _ := popup.ScrollThumb(l.Scroll.Height, l.Scroll.Total, l.Scroll.Offset)
			m.Mouse(tea.MouseClickMsg{X: x, Y: y + top, Button: tea.MouseLeft}, p)
			m.Mouse(tea.MouseMotionMsg{X: 0, Y: -5, Button: tea.MouseLeft}, p)
			m.Mouse(tea.MouseReleaseMsg{X: 0, Y: -5}, p)
			if m.Offset != 0 || m.scroll.Active {
				t.Fatal("drag did not clamp at top")
			}

			m.Mouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}, p)
			p.Height = 4
			m.Mouse(tea.MouseMotionMsg{X: 0, Y: 100, Button: tea.MouseLeft}, p)
			if m.scroll.Active || m.Offset != 0 {
				t.Fatal("resize retained invisible drag or changed selection")
			}
		}
	}
}

func TestLayoutPreservesFocusSelectionAndOpenMenuAcrossResize(t *testing.T) {
	m, p := fixture()
	m.Focus, m.Cursor, m.Offset, m.MoreOpen = 1, 23, 20, true
	m.Search.SetValue("server")
	m.Search.SetWidth(17)
	before := *m

	for _, height := range []int{24, 4, 24} {
		p.Height = height
		m.Layout(p)
		if m.Focus != before.Focus || m.Cursor != before.Cursor || m.Offset != before.Offset || m.MoreOpen != before.MoreOpen || m.Search.Width() != before.Search.Width() {
			t.Fatal("layout changed interaction state", height, m.Focus, m.Cursor, m.Offset, m.MoreOpen)
		}
	}
}

func TestRetainedRowSelectionDiffersFromKeyboardFocus(t *testing.T) {
	m, p := fixture()
	focused := m.Layout(p)
	m.Focus = 1
	filter := m.Layout(p)
	if !strings.Contains(strings.Join(focused.Rows, "\n"), "› server-00") || !strings.Contains(strings.Join(filter.Rows, "\n"), "· server-00") {
		t.Fatal("retained selection is indistinguishable from row focus")
	}
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

func TestListCardsUseMeasuredRowsAndWrapActions(t *testing.T) {
	m, p := fixture()
	p.List = true
	p.Width, p.Height = 40, 20
	p.Description = "Oldest first"
	p.Columns = []Column{{Label: "Message"}}
	p.Actions = []Action{{ID: "details", Label: "Read", Key: "enter", Enabled: true}, {ID: "edit", Label: "Edit", Key: "ctrl+e", Enabled: true}, {ID: "remove", Label: "Remove", Key: "delete", Enabled: true}, {ID: "close", Label: "Close", Key: "esc", Enabled: true}}
	for n := range m.Rows {
		m.Rows[n].Detail = "Queued · 1 image"
	}
	l := m.Layout(p)
	body := ansi.Strip(strings.Join(l.Rows, "\n"))
	if strings.Contains(body, "Filter:") || strings.Contains(body, "Message\n") {
		t.Fatal("list has table controls")
	}
	for _, label := range []string{"ENTER Read", "CTRL+E Edit", "DEL Remove", "ESC Close"} {
		if !strings.Contains(body, label) {
			t.Fatal("action clipped", body)
		}
	}
	x, y := max((p.Width-l.Width)/2, 0)+2, max((p.Height-len(l.Rows)-2)/2, 0)+1
	for _, hit := range l.Hits {
		if hit.Action == "row" && hit.Row == 1 {
			m.Mouse(tea.MouseClickMsg{X: x + hit.X0, Y: y + hit.Y, Button: tea.MouseLeft}, p)
			if m.Selected() != "1" {
				t.Fatal("card detail hit selected wrong row")
			}
		}
	}
	// A click below the thumb pages even on the final physical row of a card.
	m.Cursor, m.Offset = 0, 0
	m.Mouse(tea.MouseClickMsg{X: x + l.Width - 5, Y: y + l.BodyTop + l.BodyHeight - 1, Button: tea.MouseLeft}, p)
	if m.Cursor != l.Capacity {
		t.Fatal("list track did not page", m.Cursor, l.Capacity)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab}, p)
	if m.Focus == 1 || m.Search.Focused() {
		t.Fatal("Tab focused hidden search")
	}
}

func TestTableDescriptionCanWrapBeyondThreeLines(t *testing.T) {
	m, p := fixture()
	p.Width, p.Height = 40, 36
	p.Description = strings.Repeat("Connection details and instructions. ", 5) + "Description ends here."
	layout := m.Layout(p)
	text := ansi.Strip(strings.Join(layout.Rows, "\n"))
	if !strings.Contains(strings.Join(strings.Fields(text), " "), "Description ends here.") {
		t.Fatal("description was capped at three lines", text)
	}
	if layout.Capacity < 3 || len(layout.Rows)+2 > p.Height {
		t.Fatalf("wrapped description displaced the table: capacity=%d height=%d", layout.Capacity, len(layout.Rows)+2)
	}
}

func TestSelectUsesVisibleStableIdentity(t *testing.T) {
	model := New(textinput.New().Styles())
	model.SetRows([]Row{{ID: "hidden", Cells: []string{"Other"}}, {ID: "first", Cells: []string{"Record one"}}, {ID: "second", Cells: []string{"Record two"}}})
	model.Search.SetValue("record")
	if !model.Select("second") || model.Selected() != "second" || model.Cursor != 1 {
		t.Fatal("selection used source order instead of filtered identity")
	}
	for _, missing := range []string{"hidden", "missing"} {
		if model.Select(missing) || model.Selected() != "second" {
			t.Fatal("invisible row changed selection", missing)
		}
	}
}
