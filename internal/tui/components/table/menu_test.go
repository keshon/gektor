package table

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func overflowFixture() (*Model, Props) {
	m, p := fixture()
	p.Width, p.Height = 40, 11
	p.Actions = []Action{
		{ID: "details", Label: "Details", Key: "enter", Enabled: true},
		{ID: "connect", Label: "Connect", Key: "c", Enabled: true},
		{ID: "edit", Label: "Edit", Key: "e", Enabled: true},
		{ID: "delete", Label: "Delete", Key: "delete", Overflow: true},
		{ID: "add", Label: "Add", Key: "a", Overflow: true, Enabled: true},
		{ID: "paste", Label: "Paste JSON", Key: "p", Overflow: true, Enabled: true},
		{ID: "close", Label: "Close", Key: "esc", Enabled: true},
	}
	return m, p
}

func TestOverflowKeepsNarrowTableUsableAndPreservesSelection(t *testing.T) {
	m, p := overflowFixture()
	m.Cursor = 3
	if l := m.Layout(p); l.Capacity < 1 || strings.Contains(strings.Join(l.Rows, "\n"), "Enlarge") {
		t.Fatal("overflow actions displaced the narrow list")
	}
	m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"}, p)
	if !m.MoreOpen || m.moreRow != 1 {
		t.Fatal("More did not skip a disabled action")
	}
	m.Update(tea.PasteMsg{Content: "must not reach filter"}, p)
	if action, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDelete}, p); action != "" || m.Search.Value() != "" {
		t.Fatal("disabled menu action or paste escaped")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}, p)
	if m.MoreOpen || m.Cursor != 3 {
		t.Fatal("canceling More lost the selected row")
	}
	m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"}, p)
	if action, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"}, p); action != "paste" || m.MoreOpen {
		t.Fatal("menu shortcut missed its action")
	}
	if action, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"}, p); action != "paste" {
		t.Fatal("hidden action lost its direct shortcut")
	}
}

func TestOverflowMouseUsesAttachedGeometryAndDoesNotClickThrough(t *testing.T) {
	m, p := overflowFixture()
	m.activate("more", p)
	l := m.Layout(p)
	x, y := max((p.Width-l.Width)/2, 0)+2, max((p.Height-len(l.Rows)-2)/2, 0)+1
	row := l.Menu.Y + 1 + 2 - l.Menu.Offset
	if action := m.Mouse(tea.MouseClickMsg{X: x + l.Menu.X + 2, Y: y + row, Button: tea.MouseLeft}, p); action != "paste" {
		t.Fatal("menu click missed the drawn action", action)
	}
	m.activate("more", p)
	before := m.Cursor
	m.Mouse(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft}, p)
	if m.MoreOpen || m.Cursor != before {
		t.Fatal("outside click reached the underlying table")
	}
	m.activate("more", p)
	p.Height = 8
	m.Layout(p)
	if m.MoreOpen {
		t.Fatal("resizing left an invisible menu active")
	}
}

func TestNarrowColumnsPreserveEssentialMetadata(t *testing.T) {
	text := columns([]string{"yougile", "On demand", "12", "description"}, []Column{{Label: "Server", Width: 14}, {Label: "Status", Width: 15}, {Label: "Tools", Width: 5, HideNarrow: true}, {Label: "Description", HideNarrow: true}}, 28)
	if !strings.Contains(text, "On demand") || strings.Contains(text, "description") {
		t.Fatal("optional metadata displaced server status", text)
	}
}
